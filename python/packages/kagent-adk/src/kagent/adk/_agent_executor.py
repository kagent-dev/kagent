from __future__ import annotations

import asyncio
import inspect
import logging
import uuid
from contextlib import suppress
from dataclasses import dataclass
from typing import Any, Awaitable, Callable, Optional

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events import Event as A2AEvent
from a2a.server.events.event_queue_v2 import EventQueue
from a2a.types import Message, Part, Role, TaskState, TaskStatus, TaskStatusUpdateEvent
from google.adk.a2a.converters.part_converter import A2APartToGenAIPartConverter
from google.adk.a2a.converters.request_converter import (
    AgentRunRequest,
    convert_a2a_request_to_agent_run_request,
)
from google.adk.a2a.executor.a2a_agent_executor import A2aAgentExecutor as UpstreamA2aAgentExecutor
from google.adk.a2a.executor.config import A2aAgentExecutorConfig as UpstreamA2aAgentExecutorConfig
from google.adk.a2a.executor.config import ExecuteInterceptor
from google.adk.a2a.executor.executor_context import ExecutorContext
from google.adk.agents.run_config import StreamingMode
from google.adk.events import Event
from google.adk.runners import Runner
from google.genai import types as genai_types
from kagent.core.a2a import (
    HITL_TYPE_ASK_USER_RESPONSE,
    HITL_TYPE_TOOL_APPROVAL_RESPONSE,
    get_hitl_payload,
    get_kagent_metadata_key,
    hitl_activated,
    now_timestamp,
)
from kagent.core.tracing import merge_caller_context_attributes
from kagent.core.tracing._span_processor import clear_kagent_span_attributes, set_kagent_span_attributes
from pydantic import BaseModel

from ._bearer_token import bearer_token, extract_bearer_token
from ._hitl import build_hitl_status_message, build_resume_hitl_message
from ._mcp_toolset import is_anyio_cross_task_cancel_scope_error
from .converters.event_converter import serialize_metadata_value

logger = logging.getLogger("kagent_adk." + __name__)


class A2aAgentExecutorConfig(BaseModel):
    """Kagent-specific configuration around the upstream executor."""

    stream: bool = False


@dataclass
class _ExecutionState:
    request_context: RequestContext
    invocation_id: str | None = None
    last_usage_metadata: Any = None


def _call_state(context: RequestContext) -> dict[str, Any]:
    state = getattr(context.call_context, "state", None)
    return state if isinstance(state, dict) else {}


def _friendly_error_message(error_message: str) -> str:
    if (
        "JSONDecodeError" in error_message
        or "Unterminated string" in error_message
        or "APIConnectionError" in error_message
    ) and ("function_call" in error_message.lower() or "json.loads" in error_message):
        return (
            "The model does not support function calling properly. "
            "This error typically occurs when using Ollama models with tools. "
            "Please either:\n"
            "1. Remove tools from the agent configuration, or\n"
            "2. Use a model that supports function calling (e.g., OpenAI, Anthropic, or Gemini models)."
        )
    return error_message


class A2aAgentExecutor(AgentExecutor):
    """Thin kagent adapter around ADK 2.x's upstream A2A executor.

    Upstream owns request conversion, long-running function handling, event
    conversion, task state transitions, and current-task continuation. Kagent
    retains only its public HITL extension boundary, per-request runner/MCP
    lifecycle, session metadata, request headers, telemetry, and UI metadata.
    """

    def __init__(
        self,
        *,
        runner: Callable[..., Runner | Awaitable[Runner]],
        config: Optional[A2aAgentExecutorConfig] = None,
    ):
        self._runner = runner
        self._kagent_config = config or A2aAgentExecutorConfig()

    async def _resolve_runner(self) -> Runner:
        if not callable(self._runner):
            raise TypeError(f"Runner must be a callable that returns a Runner, got {type(self._runner)}")
        result = self._runner()
        resolved_runner = await result if inspect.isawaitable(result) else result
        if not isinstance(resolved_runner, Runner):
            raise TypeError(f"Callable must return a Runner instance, got {type(resolved_runner)}")
        return resolved_runner

    async def cancel(self, context: RequestContext, event_queue: EventQueue) -> None:
        executor = UpstreamA2aAgentExecutor(
            runner=self._runner,
            force_new_version=True,
        )
        await executor.cancel(context, event_queue)

    async def execute(self, context: RequestContext, event_queue: EventQueue) -> None:
        if not context.message:
            raise ValueError("A2A request must have a message")

        runner: Runner | None = None
        context_token = None
        try:
            self._translate_hitl_response(context)
            runner = await self._resolve_runner()

            run_request = self._convert_request(context, None)
            await self._prepare_session(context, run_request, runner)

            span_attributes = {
                "kagent.user_id": run_request.user_id,
                "gen_ai.task.id": context.task_id,
                "gen_ai.conversation.id": run_request.session_id,
            }
            # Allowlisted caller context joins the request-scoped bag rather
            # than a single span, so tool, sub-agent, and model spans all
            # carry it. Fill-if-absent so a caller cannot override kagent.user_id.
            merge_caller_context_attributes(span_attributes, message=context.message)
            context_token = set_kagent_span_attributes(
                {key: value for key, value in span_attributes.items() if value is not None}
            )

            execution_state = _ExecutionState(request_context=context)
            upstream_config = UpstreamA2aAgentExecutorConfig(
                request_converter=self._convert_request,
                execute_interceptors=[
                    ExecuteInterceptor(
                        after_event=lambda executor_context, event, adk_event: self._after_event(
                            execution_state,
                            executor_context,
                            event,
                            adk_event,
                        ),
                        after_agent=lambda executor_context, event: self._after_agent(
                            execution_state,
                            executor_context,
                            event,
                        ),
                    )
                ],
            )
            executor = UpstreamA2aAgentExecutor(
                runner=runner,
                config=upstream_config,
                force_new_version=True,
            )
            await executor.execute(context, event_queue)
        except asyncio.CancelledError as error:
            current_task = asyncio.current_task()
            if current_task is not None:
                while current_task.uncancel() > 0:
                    pass
            logger.error("A2A request execution was cancelled", exc_info=True)
            await self._publish_failed_status_event(
                context,
                event_queue,
                str(error) or "A2A request execution was cancelled.",
            )
        except Exception as error:
            logger.error("Error preparing A2A request: %s", error, exc_info=True)
            await self._publish_failed_status_event(context, event_queue, _friendly_error_message(str(error)))
        finally:
            if context_token is not None:
                clear_kagent_span_attributes(context_token)
            if runner is not None:
                await self._safe_close_runner(runner)

    def _translate_hitl_response(self, context: RequestContext) -> None:
        payload = get_hitl_payload(context.message)
        if not payload or payload.get("type") not in {
            HITL_TYPE_TOOL_APPROVAL_RESPONSE,
            HITL_TYPE_ASK_USER_RESPONSE,
        }:
            return
        if context.current_task is None:
            raise ValueError("HITL decision requires a stored current task")
        resume_message = build_resume_hitl_message(context.current_task, context.message)
        context.message.CopyFrom(resume_message)

    def _convert_request(
        self,
        context: RequestContext,
        part_converter: A2APartToGenAIPartConverter | None,
    ) -> AgentRunRequest:
        if part_converter is None:
            run_request = convert_a2a_request_to_agent_run_request(context)
        else:
            run_request = convert_a2a_request_to_agent_run_request(context, part_converter)
        run_config = run_request.run_config
        if run_config is None:
            raise ValueError("ADK request converter did not create a run config")
        run_request.run_config = run_config.model_copy(
            update={
                "streaming_mode": StreamingMode.SSE if self._kagent_config.stream else StreamingMode.NONE,
            }
        )
        headers = _call_state(context).get("headers", {})
        headers = headers if isinstance(headers, dict) else {}
        run_request.state_delta = {"headers": headers}
        # Also stash the token in a ContextVar for consumers with no
        # callback_context of their own - see _bearer_token.py.
        bearer_token.set(extract_bearer_token(headers))
        return run_request

    async def _prepare_session(
        self,
        context: RequestContext,
        run_request: AgentRunRequest,
        runner: Runner,
    ) -> None:
        if not run_request.user_id or not run_request.session_id:
            raise ValueError("A2A request is missing user or session identity")
        session = await runner.session_service.get_session(
            app_name=runner.app_name,
            user_id=run_request.user_id,
            session_id=run_request.session_id,
        )
        if session is not None:
            return

        session_name = None
        for part in context.message.parts if context.message else []:
            if part.HasField("text") and part.text:
                text = part.text.strip()
                session_name = text[:20] + ("..." if len(text) > 20 else "")
                break
        state: dict[str, Any] = {"session_name": session_name}
        source = _call_state(context).get("kagent_source")
        if source:
            state["source"] = source
        session = await runner.session_service.create_session(
            app_name=runner.app_name,
            user_id=run_request.user_id,
            state=state,
            session_id=run_request.session_id,
        )
        run_request.session_id = session.id

    async def _after_event(
        self,
        state: _ExecutionState,
        executor_context: ExecutorContext,
        event: A2AEvent,
        adk_event: Event,
    ) -> A2AEvent:
        del executor_context
        if adk_event.invocation_id:
            state.invocation_id = adk_event.invocation_id
        if adk_event.usage_metadata is not None:
            state.last_usage_metadata = adk_event.usage_metadata
        return event

    async def _after_agent(
        self,
        state: _ExecutionState,
        executor_context: ExecutorContext,
        event: TaskStatusUpdateEvent,
    ) -> TaskStatusUpdateEvent:
        metadata: dict[str, Any] = {
            get_kagent_metadata_key("app_name"): executor_context.app_name,
            get_kagent_metadata_key("user_id"): executor_context.user_id,
            get_kagent_metadata_key("session_id"): executor_context.session_id,
        }
        if state.invocation_id:
            metadata[get_kagent_metadata_key("invocation_id")] = state.invocation_id
        if state.last_usage_metadata is not None:
            metadata[get_kagent_metadata_key("usage_metadata")] = serialize_metadata_value(state.last_usage_metadata)
        event.metadata.update(metadata)

        if event.status.state == TaskState.TASK_STATE_INPUT_REQUIRED and event.status.message:
            headers = _call_state(state.request_context).get("headers", {})
            public_message = build_hitl_status_message(
                list(event.status.message.parts),
                state.request_context.task_id,
                state.request_context.context_id,
                hitl_activated(headers if isinstance(headers, dict) else {}),
            )
            event.status.message.CopyFrom(public_message)
        elif event.status.state == TaskState.TASK_STATE_FAILED and event.status.message:
            for part in event.status.message.parts:
                if part.HasField("text") and part.text:
                    part.text = _friendly_error_message(part.text)
        return event

    async def _safe_close_runner(self, runner: Runner) -> None:
        cleanup_task = asyncio.create_task(runner.close())
        try:
            results = await asyncio.gather(cleanup_task, return_exceptions=True)
        except asyncio.CancelledError:
            cleanup_task.cancel()
            with suppress(asyncio.CancelledError):
                await cleanup_task
            raise

        for result in results:
            if not isinstance(result, BaseException):
                continue
            if isinstance(result, (KeyboardInterrupt, SystemExit, asyncio.CancelledError)):
                raise result
            if is_anyio_cross_task_cancel_scope_error(result):
                logger.warning(
                    "Non-fatal anyio cancel scope error during runner cleanup: %s: %s",
                    type(result).__name__,
                    result,
                )
                continue
            raise result

    async def _publish_failed_status_event(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        error_message: str,
    ) -> None:
        try:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    status=TaskStatus(
                        state=TaskState.TASK_STATE_FAILED,
                        timestamp=now_timestamp(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.ROLE_AGENT,
                            parts=[Part(text=error_message)],
                        ),
                    ),
                )
            )
        except BaseException as enqueue_error:
            if isinstance(enqueue_error, (KeyboardInterrupt, SystemExit)):
                raise
            logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)
