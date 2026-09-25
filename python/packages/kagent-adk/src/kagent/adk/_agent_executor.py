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
from a2a.types import (
    Artifact,
    Message,
    Part,
    Role,
    SendMessageRequest,
    Task,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from google.adk.a2a.converters.part_converter import (
    A2APartToGenAIPartConverter,
)
from google.adk.a2a.converters.part_converter import (
    convert_a2a_part_to_genai_part as convert_upstream_a2a_part_to_genai_part,
)
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
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    A2A_PART_TYPE_METADATA_KEY,
    A2A_USAGE_METADATA_KEY,
    HITL_TYPE_ASK_USER_RESPONSE,
    HITL_TYPE_TOOL_APPROVAL_RESPONSE,
    get_hitl_payload,
    hitl_activated,
    now_timestamp,
)
from kagent.core.tracing._span_processor import clear_kagent_span_attributes, set_kagent_span_attributes
from pydantic import BaseModel

from ._bearer_token import bearer_token, extract_bearer_token
from ._hitl import build_hitl_status_message, build_resume_hitl_message
from ._mcp_toolset import is_anyio_cross_task_cancel_scope_error
from ._request_identity import public_context_id, request_user_id
from .converters.event_converter import serialize_metadata_value
from .converters.part_converter import convert_a2a_part_to_genai_part as convert_kagent_a2a_part_to_genai_part

logger = logging.getLogger("kagent_adk." + __name__)

_ADK_METADATA_PREFIX = "adk_"
_ADK_PART_TYPE_METADATA_KEY = "adk_type"
_ADK_USAGE_METADATA_KEY = "adk_usage_metadata"


class A2aAgentExecutorConfig(BaseModel):
    """Kagent-specific configuration around the upstream executor."""

    stream: bool = False


@dataclass
class _ExecutionState:
    request_context: RequestContext
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


def _canonicalize_adk_metadata(metadata: Any, *, part: bool = False) -> None:
    """Translate public ADK semantics and discard the remaining SDK metadata."""
    if not metadata:
        return
    values = MessageToDict(metadata) if hasattr(metadata, "DESCRIPTOR") else dict(metadata)
    if part and _ADK_PART_TYPE_METADATA_KEY in values:
        metadata.update({A2A_PART_TYPE_METADATA_KEY: values[_ADK_PART_TYPE_METADATA_KEY]})
    if _ADK_USAGE_METADATA_KEY in values:
        metadata.update({A2A_USAGE_METADATA_KEY: values[_ADK_USAGE_METADATA_KEY]})
    for key in list(metadata):
        if key.startswith(_ADK_METADATA_PREFIX):
            del metadata[key]


def _canonicalize_adk_message(message: Message | None) -> None:
    if message is None:
        return
    _canonicalize_adk_metadata(message.metadata)
    for part in message.parts:
        _canonicalize_adk_metadata(part.metadata, part=True)


def _canonicalize_adk_artifact(artifact: Artifact | None) -> None:
    if artifact is None:
        return
    _canonicalize_adk_metadata(artifact.metadata)
    for part in artifact.parts:
        _canonicalize_adk_metadata(part.metadata, part=True)


def _canonicalize_adk_event(event: A2AEvent) -> None:
    _canonicalize_adk_metadata(event.metadata)
    if isinstance(event, TaskArtifactUpdateEvent):
        _canonicalize_adk_artifact(event.artifact)
    elif isinstance(event, TaskStatusUpdateEvent):
        _canonicalize_adk_message(event.status.message if event.status else None)
    elif isinstance(event, Message):
        _canonicalize_adk_message(event)
    elif isinstance(event, Task):
        _canonicalize_adk_message(event.status.message if event.status else None)
        for message in event.history:
            _canonicalize_adk_message(message)
        for artifact in event.artifacts:
            _canonicalize_adk_artifact(artifact)


def _convert_public_a2a_part_to_genai_part(part: Part) -> genai_types.Part | list[genai_types.Part] | None:
    """Convert canonical typed data locally and delegate all other parts to ADK."""
    metadata = MessageToDict(part.metadata) if part.metadata else {}
    if part.HasField("data") and A2A_PART_TYPE_METADATA_KEY in metadata:
        return convert_kagent_a2a_part_to_genai_part(part)
    return convert_upstream_a2a_part_to_genai_part(part)


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
        identity_token = public_context_id.set(context.context_id)
        user_token = None
        try:
            context = self._translate_hitl_response(context)
            runner = await self._resolve_runner()

            run_request = self._convert_request(context, _convert_public_a2a_part_to_genai_part)
            user_token = request_user_id.set(run_request.user_id)
            await self._prepare_session(context, run_request, runner)

            span_attributes = {
                "kagent.user_id": run_request.user_id,
                "gen_ai.task.id": context.task_id,
                "gen_ai.conversation.id": context.context_id,
            }
            context_token = set_kagent_span_attributes(
                {key: value for key, value in span_attributes.items() if value is not None}
            )

            execution_state = _ExecutionState(request_context=context)
            upstream_config = UpstreamA2aAgentExecutorConfig(
                a2a_part_converter=_convert_public_a2a_part_to_genai_part,
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
            public_context_id.reset(identity_token)
            if user_token is not None:
                request_user_id.reset(user_token)
            if context_token is not None:
                clear_kagent_span_attributes(context_token)
            if runner is not None:
                await self._safe_close_runner(runner)

    def _translate_hitl_response(self, context: RequestContext) -> RequestContext:
        payload = get_hitl_payload(context.message)
        if not payload or payload.get("type") not in {
            HITL_TYPE_TOOL_APPROVAL_RESPONSE,
            HITL_TYPE_ASK_USER_RESPONSE,
        }:
            return context
        if context.current_task is None:
            raise ValueError("HITL decision requires a stored current task")
        resume_message = build_resume_hitl_message(context.current_task, context.message)
        # The SDK's event consumer retains the public request while native work
        # runs. Keep the translated native request separate so persistence
        # retains the caller's original message.
        return RequestContext(
            call_context=context.call_context,
            request=SendMessageRequest(
                message=resume_message,
                configuration=context.configuration,
                metadata=context.metadata,
            ),
            task_id=context.task_id,
            context_id=context.context_id,
            task=context.current_task,
            related_tasks=context.related_tasks,
        )

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
        if adk_event.usage_metadata is not None:
            state.last_usage_metadata = adk_event.usage_metadata
        _canonicalize_adk_event(event)
        return event

    async def _after_agent(
        self,
        state: _ExecutionState,
        executor_context: ExecutorContext,
        event: TaskStatusUpdateEvent,
    ) -> TaskStatusUpdateEvent:
        del executor_context
        metadata: dict[str, Any] = {}
        if state.last_usage_metadata is not None:
            metadata[A2A_USAGE_METADATA_KEY] = serialize_metadata_value(state.last_usage_metadata)
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
        _canonicalize_adk_event(event)
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
