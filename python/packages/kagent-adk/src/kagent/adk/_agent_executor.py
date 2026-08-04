from __future__ import annotations

import asyncio
import inspect
import logging
import uuid
from contextlib import suppress
from typing import Any, Awaitable, Callable, Optional

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue_v2 import EventQueue
from a2a.types import (
    Message,
    Part,
    Role,
    Task,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from google.adk.events import Event, EventActions
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME, REQUEST_EUC_FUNCTION_CALL_NAME
from google.adk.runners import Runner
from google.adk.sessions import Session
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.adk.utils.context_utils import Aclosing
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    AskUserRequest,
    ToolApprovalRequest,
    ToolApprovalResponse,
    get_ask_user_response,
    get_kagent_metadata_key,
    get_tool_approval_response,
    hitl_activated,
    now_timestamp,
)
from kagent.core.tracing._span_processor import clear_kagent_span_attributes, set_kagent_span_attributes
from pydantic import BaseModel

from ._hitl import build_hitl_status_message, get_remote_hitl_state, visible_tools
from ._mcp_toolset import is_anyio_cross_task_cancel_scope_error
from ._remote_a2a_tool import SubagentSessionProvider
from .converters.event_converter import convert_event_to_a2a_events, serialize_metadata_value
from .converters.request_converter import convert_a2a_request_to_adk_run_args

logger = logging.getLogger("kagent_adk." + __name__)


def _is_long_running_function_call(part: Part) -> bool:
    """True when a DataPart is a long-running function_call (HITL/auth)."""
    if not part.HasField("data"):
        return False
    metadata = MessageToDict(part.metadata) if part.metadata else {}
    return bool(metadata.get(get_kagent_metadata_key("is_long_running")))


def _split_hitl_artifact_parts(
    event: TaskArtifactUpdateEvent,
    hitl_parts: list[Part],
) -> TaskArtifactUpdateEvent | None:
    """Move long-running function_call parts onto the HITL status; keep the rest.

    Mirrors Go adka2a inputRequiredProcessor: confirmation/auth parts belong on
    input-required/auth-required status, while ordinary text/tool output stays
    on the artifact stream.
    """
    output_parts: list[Part] = []
    for part in event.artifact.parts:
        if _is_long_running_function_call(part):
            hitl_parts.append(part)
        else:
            output_parts.append(part)
    if not output_parts:
        return None
    del event.artifact.parts[:]
    event.artifact.parts.extend(output_parts)
    return event


class A2aAgentExecutorConfig(BaseModel):
    """Configuration for the KAgent A2aAgentExecutor."""

    stream: bool = False


class A2aAgentExecutor(AgentExecutor):
    """KAgent's A2A agent executor.

    Extends the upstream google-adk A2aAgentExecutor with:
    - Per-request runner lifecycle (created fresh and closed after each request)
    - OpenTelemetry span attribute management
    - Enhanced error handling (Ollama-specific JSON parse errors, CancelledError)
    - A2A artifact streaming with kagent HITL status handling
    - Session naming from first message text
    - Request header forwarding to session state
    - Invocation ID tracking in final event metadata
    """

    def __init__(
        self,
        *,
        runner: Callable[..., Runner | Awaitable[Runner]],
        config: Optional[A2aAgentExecutorConfig] = None,
        task_store=None,
    ):
        self._runner = runner
        self._kagent_config = config
        self._task_store = task_store

    async def _resolve_runner(self) -> Runner:
        """Resolve the runner from the callable.

        Unlike the upstream executor which caches a single Runner instance,
        kagent always creates a fresh Runner per request. This is necessary
        because MCP toolset connections are not shared between requests and
        must be cleaned up after each execution.
        """
        if callable(self._runner):
            result = self._runner()

            if inspect.iscoroutine(result):
                resolved_runner = await result
            else:
                resolved_runner = result

            if not isinstance(resolved_runner, Runner):
                raise TypeError(f"Callable must return a Runner instance, got {type(resolved_runner)}")

            return resolved_runner

        raise TypeError(
            f"Runner must be a Runner instance or a callable that returns a Runner, got {type(self._runner)}"
        )

    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not supported")

    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Executes an A2A request and publishes updates to the event queue
        specified. It runs as following:
        * Takes the input from the A2A request
        * Convert the input to ADK input content, and runs the ADK agent
        * Collects output events of the underlying ADK Agent
        * Converts the ADK output events into A2A task updates
        * Publishes the updates back to A2A server via event queue
        """
        try:
            await self._execute_impl(context, event_queue)
        except asyncio.CancelledError as e:
            # anyio cancel scope corruption (from MCP session cleanup in a
            # different task context) calls Task.cancel() on the current
            # task. CancelledError can escape from multiple places: the
            # outer try body, the inner except handler (if the task's
            # cancellation counter > 1), or the finally block's
            # _safe_close_runner (which re-raises CancelledError).
            # This top-level guard ensures CancelledError never propagates
            # to _run_event_stream in the A2A SDK, which would produce a
            # 500 Internal Server Error.
            current_task = asyncio.current_task()
            if current_task is not None:
                # Clear all pending cancellation requests so subsequent
                # awaits (e.g. publishing the failure event) don't re-raise.
                while current_task.uncancel() > 0:
                    pass
            logger.error(
                "CancelledError escaped execute, converting to failed status: %s",
                e,
                exc_info=True,
            )
            await self._publish_failed_status_event(
                context,
                event_queue,
                str(e) or "A2A request execution was cancelled.",
            )

    async def _execute_impl(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        if not context.message:
            raise ValueError("A2A request must have a message")

        # Convert the a2a request to ADK run args
        stream = self._kagent_config.stream if self._kagent_config is not None else False
        run_args = convert_a2a_request_to_adk_run_args(context, stream=stream)

        # Prepare span attributes.
        span_attributes = {}
        if run_args.get("user_id"):
            span_attributes["kagent.user_id"] = run_args["user_id"]
        if context.task_id:
            span_attributes["gen_ai.task.id"] = context.task_id
        if run_args.get("session_id"):
            span_attributes["gen_ai.conversation.id"] = run_args["session_id"]

        # Set kagent span attributes for all spans in context.
        context_token = set_kagent_span_attributes(span_attributes)
        runner: Optional[Runner] = None
        try:
            # for new task, create a task submitted event
            if not context.current_task:
                await event_queue.enqueue_event(
                    Task(
                        id=context.task_id,
                        context_id=context.context_id,
                        status=TaskStatus(
                            state=TaskState.TASK_STATE_SUBMITTED,
                            message=context.message,
                            timestamp=now_timestamp(),
                        ),
                    )
                )

            # Handle the request and publish updates to the event queue
            runner = await self._resolve_runner()
            try:
                await self._handle_request(context, event_queue, runner, run_args)
            except asyncio.CancelledError as e:
                logger.error("A2A request execution was cancelled", exc_info=True)
                error_message = str(e) or "A2A request execution was cancelled."
                await self._publish_failed_status_event(context, event_queue, error_message)
            except Exception as e:
                logger.error("Error handling A2A request: %s", e, exc_info=True)

                # Check if this is a LiteLLM JSON parsing error (common with Ollama models that don't support function calling)
                error_message = str(e)
                if (
                    "JSONDecodeError" in error_message
                    or "Unterminated string" in error_message
                    or "APIConnectionError" in error_message
                ):
                    # Check if it's related to function calling
                    if "function_call" in error_message.lower() or "json.loads" in error_message:
                        error_message = (
                            "The model does not support function calling properly. "
                            "This error typically occurs when using Ollama models with tools. "
                            "Please either:\n"
                            "1. Remove tools from the agent configuration, or\n"
                            "2. Use a model that supports function calling (e.g., OpenAI, Anthropic, or Gemini models)."
                        )
                # Publish failure event
                await self._publish_failed_status_event(context, event_queue, error_message)
        finally:
            clear_kagent_span_attributes(context_token)
            # close the runner which cleans up the mcptoolsets
            # since the runner is created for each a2a request
            # and the mcptoolsets are not shared between requests
            # this is necessary to gracefully handle mcp toolset connections
            if runner is not None:
                await self._safe_close_runner(runner)

    async def _safe_close_runner(self, runner: Runner):
        """Close the runner in an isolated task to prevent cancel scope
        corruption from propagating to the caller.

        MCP session cleanup can trigger anyio CancelScope violations when
        cancel scopes are entered in one task context but exited in another
        (e.g. via asyncio.wait_for creating a subtask). Running the cleanup
        in a separate task and collecting exceptions via asyncio.gather
        ensures cleanup runs in a separate task context. We only suppress
        the known non-fatal anyio cross-task cancel scope cleanup error and
        re-raise everything else.

        See: https://github.com/kagent-dev/kagent/issues/1276
        """
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
            if isinstance(result, (KeyboardInterrupt, SystemExit)):
                raise result
            if isinstance(result, asyncio.CancelledError):
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
                    status=TaskStatus(
                        state=TaskState.TASK_STATE_FAILED,
                        timestamp=now_timestamp(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.ROLE_AGENT,
                            parts=[Part(text=error_message)],
                        ),
                    ),
                    context_id=context.context_id,
                )
            )
        except BaseException as enqueue_error:
            if isinstance(enqueue_error, (KeyboardInterrupt, SystemExit)):
                raise
            logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)

    # TODO(adk-2.0): Delete this session scan and route the extension response
    # through upstream ADK's current-task long-running-function resume path.
    @staticmethod
    def _find_pending_confirmations(session: Session) -> dict[str, dict[str, Any] | None]:
        """Find unanswered confirmations for the pre-ADK-2.0 resume path."""
        pending: dict[str, dict[str, Any] | None] = {}
        responded_ids: set[str] = set()

        for event in reversed(session.events or []):
            for response in event.get_function_responses():
                if response.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME and response.id is not None:
                    responded_ids.add(response.id)

            for call in event.get_function_calls():
                if call.name != REQUEST_CONFIRMATION_FUNCTION_CALL_NAME or call.id is None:
                    continue
                payload = None
                if call.args and isinstance(call.args, dict):
                    tool_confirmation = call.args.get("toolConfirmation")
                    if isinstance(tool_confirmation, dict) and isinstance(tool_confirmation.get("payload"), dict):
                        payload = dict(tool_confirmation["payload"])
                pending[call.id] = payload

            if pending:
                break

        # Remove the ones that have already been responded to
        for responded_id in responded_ids:
            pending.pop(responded_id, None)

        return pending

    @staticmethod
    def _merge_confirmation_payload(
        original_payload: dict[str, Any] | None,
        extra: dict[str, Any] | None,
    ) -> dict[str, Any] | None:
        if not original_payload and not extra:
            return None
        return {**(original_payload or {}), **(extra or {})}

    @staticmethod
    def _function_response(call_id: str, confirmation: ToolConfirmation) -> genai_types.Part:
        return genai_types.Part(
            function_response=genai_types.FunctionResponse(
                name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                id=call_id,
                response={"response": confirmation.model_dump_json()},
            )
        )

    def _process_hitl_response(self, session: Session, message: Message) -> list[genai_types.Part] | None:
        """Translate an extension response for the pre-ADK-2.0 executor."""
        pending = self._find_pending_confirmations(session)
        if not pending:
            return None

        ask_response = get_ask_user_response(message)
        if ask_response is not None:
            for call_id, original_payload in pending.items():
                remote_state = get_remote_hitl_state(original_payload)
                expected_id = call_id
                if remote_state is not None:
                    if not isinstance(remote_state.hitl_request, AskUserRequest):
                        continue
                    expected_id = remote_state.hitl_request.id
                if ask_response.id != expected_id:
                    continue
                extra = (
                    {"hitl_response": ask_response.model_dump(exclude_none=True)}
                    if remote_state is not None
                    else {"answers": ask_response.answers}
                )
                payload = self._merge_confirmation_payload(original_payload, extra)
                return [self._function_response(call_id, ToolConfirmation(confirmed=True, payload=payload))]
            raise ValueError(f"Unknown ask_user response id: {ask_response.id}")

        approval_response = get_tool_approval_response(message)
        if approval_response is None:
            return None
        approvals = {approval.id: approval for approval in approval_response.approvals}
        if len(approvals) != len(approval_response.approvals):
            raise ValueError("Tool approval response contains duplicate ids")

        parts: list[genai_types.Part] = []
        for call_id, original_payload in pending.items():
            remote_state = get_remote_hitl_state(original_payload)
            if remote_state is not None:
                if not isinstance(remote_state.hitl_request, ToolApprovalRequest):
                    raise ValueError("Tool approval response received for a pending ask_user request")
                nested_approvals = []
                for tool in visible_tools(remote_state.hitl_request):
                    approval = approvals.pop(tool.id, None)
                    if approval is None:
                        raise ValueError(f"Tool approval response is missing id: {tool.id}")
                    nested_approvals.append(approval)
                nested_response = ToolApprovalResponse(approvals=nested_approvals)
                payload = self._merge_confirmation_payload(
                    original_payload,
                    {"hitl_response": nested_response.model_dump(exclude_none=True)},
                )
                confirmation = ToolConfirmation(
                    confirmed=all(approval.approved for approval in nested_approvals),
                    payload=payload,
                )
            else:
                approval = approvals.pop(call_id, None)
                if approval is None:
                    raise ValueError(f"Tool approval response is missing id: {call_id}")
                extra = None
                if not approval.approved and approval.rejection_reason:
                    extra = {"rejection_reason": approval.rejection_reason}
                confirmation = ToolConfirmation(
                    confirmed=approval.approved,
                    payload=self._merge_confirmation_payload(original_payload, extra),
                )
            parts.append(self._function_response(call_id, confirmation))

        if approvals:
            raise ValueError(f"Tool approval response contains unknown ids: {', '.join(sorted(approvals))}")
        return parts

    async def _handle_request(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        runner: Runner,
        run_args: dict[str, Any],
    ) -> None:
        # ensure the session exists
        session = await self._prepare_session(context, run_args, runner)

        # HITL resume: translate A2A approval/rejection to ADK FunctionResponse
        if get_tool_approval_response(context.message) or get_ask_user_response(context.message):
            parts = self._process_hitl_response(session, context.message)
            if parts:
                run_args["new_message"] = genai_types.Content(role="user", parts=parts)
            # Fall through to normal execution with the constructed FunctionResponse
        else:
            # Normal flow: set request headers to session state
            headers = context.call_context.state.get("headers", {})
            state_changes = {
                "headers": headers,
            }

            actions_with_update = EventActions(state_delta=state_changes)
            system_event = Event(
                invocation_id="header_update",
                author="system",
                actions=actions_with_update,
            )

            await runner.session_service.append_event(session, system_event)

        # create invocation context
        invocation_context = runner._new_invocation_context(
            session=session,
            new_message=run_args["new_message"],
            run_config=run_args["run_config"],
        )

        # Base metadata for events (invocation_id will be updated once we see it from ADK)
        run_metadata = {
            get_kagent_metadata_key("app_name"): runner.app_name,
            get_kagent_metadata_key("user_id"): run_args["user_id"],
            get_kagent_metadata_key("session_id"): run_args["session_id"],
        }

        # publish the task working event
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.TASK_STATE_WORKING,
                    timestamp=now_timestamp(),
                ),
                context_id=context.context_id,
                metadata=run_metadata.copy(),
            )
        )

        # Track the invocation_id from ADK events
        # For streaming A2A update events, the invocation_id is added through event converter
        # This adds the invocation_id of the run to the metadata of the FINAL event (completed or failed)
        real_invocation_id: str | None = None
        last_usage_metadata = None

        # Build a mapping of tool name -> subagent session ID once so the
        # event converter can stamp it onto function_call DataParts.
        subagent_session_ids: dict[str, str] = {}
        for tool in getattr(runner.agent, "tools", None) or []:
            if isinstance(tool, SubagentSessionProvider) and tool.subagent_session_id:
                subagent_session_ids[tool.name] = tool.subagent_session_id

        hitl_parts: list[Part] = []
        terminal_status: TaskStatusUpdateEvent | None = None
        agents_artifacts: dict[str, str] = {}
        async with Aclosing(runner.run_async(**run_args)) as agen:
            async for adk_event in agen:
                # Capture the real invocation_id from the first ADK event that has one
                event_inv_id = getattr(adk_event, "invocation_id", None)
                if event_inv_id and not real_invocation_id:
                    real_invocation_id = event_inv_id
                    run_metadata[get_kagent_metadata_key("invocation_id")] = real_invocation_id

                # Track the last usage_metadata so it can be included in the final
                # event's run_metadata. The A2A task_manager merges run_metadata into
                # task.metadata, making it available to callers (e.g. KAgentRemoteA2ATool).
                if getattr(adk_event, "usage_metadata", None) is not None:
                    last_usage_metadata = adk_event.usage_metadata

                a2a_events = convert_event_to_a2a_events(
                    adk_event,
                    invocation_context,
                    context.task_id,
                    context.context_id,
                    subagent_session_ids=subagent_session_ids or None,
                    agents_artifacts=agents_artifacts,
                )

                is_long_running = bool(getattr(adk_event, "long_running_tool_ids", None))
                for a2a_event in a2a_events:
                    if isinstance(a2a_event, TaskStatusUpdateEvent) and a2a_event.status.state in (
                        TaskState.TASK_STATE_FAILED,
                        TaskState.TASK_STATE_AUTH_REQUIRED,
                        TaskState.TASK_STATE_INPUT_REQUIRED,
                    ):
                        terminal_status = a2a_event
                    elif is_long_running and isinstance(a2a_event, TaskArtifactUpdateEvent):
                        a2a_event = _split_hitl_artifact_parts(a2a_event, hitl_parts)
                        if a2a_event is None:
                            continue
                    await event_queue.enqueue_event(a2a_event)

                if terminal_status is not None:
                    break

                # Break on confirmation events that use long running tools
                if is_long_running:
                    break

        # Attach the last LLM usage to run_metadata so the A2A task_manager
        # merges it into task.metadata on the completed Task object.
        if last_usage_metadata is not None:
            run_metadata[get_kagent_metadata_key("usage_metadata")] = serialize_metadata_value(last_usage_metadata)

        if hitl_parts:
            hitl_state = TaskState.TASK_STATE_INPUT_REQUIRED
            for part in hitl_parts:
                if not part.HasField("data"):
                    continue
                payload = MessageToDict(part.data)
                if isinstance(payload, dict) and payload.get("name") == REQUEST_EUC_FUNCTION_CALL_NAME:
                    hitl_state = TaskState.TASK_STATE_AUTH_REQUIRED
                    break
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    status=TaskStatus(
                        state=hitl_state,
                        timestamp=now_timestamp(),
                        message=build_hitl_status_message(
                            hitl_parts,
                            context.task_id,
                            context.context_id,
                            hitl_activated(context.call_context.state.get("headers", {})),
                        ),
                    ),
                    metadata=run_metadata,
                )
            )
        elif terminal_status is None:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.TASK_STATE_COMPLETED,
                        timestamp=now_timestamp(),
                    ),
                    context_id=context.context_id,
                    metadata=run_metadata,
                )
            )

    async def _prepare_session(self, context: RequestContext, run_args: dict[str, Any], runner: Runner):
        session_id = run_args["session_id"]
        # create a new session if not exists
        user_id = run_args["user_id"]
        session = await runner.session_service.get_session(
            app_name=runner.app_name,
            user_id=user_id,
            session_id=session_id,
        )

        if session is None:
            # Extract session name from the first TextPart (like the UI does)
            session_name = None
            if context.message and context.message.parts:
                for part in context.message.parts:
                    if part.HasField("text") and part.text:
                        # Take first 20 chars + "..." if longer (matching UI behavior)
                        text = part.text.strip()
                        session_name = text[:20] + ("..." if len(text) > 20 else "")
                        break

            state: dict[str, Any] = {"session_name": session_name}
            # Propagate source (e.g. "agent") so the session is tagged in the DB.
            source = None
            if context.call_context and context.call_context.state:
                source = context.call_context.state.get("kagent_source")
            if source:
                state["source"] = source

            session = await runner.session_service.create_session(
                app_name=runner.app_name,
                user_id=user_id,
                state=state,
                session_id=session_id,
            )

            # Update run_args with the new session_id
            run_args["session_id"] = session.id

        return session
