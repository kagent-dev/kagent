from __future__ import annotations

import asyncio
import inspect
import logging
import uuid
from contextlib import suppress
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable, Optional

from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from google.adk.a2a.executor.a2a_agent_executor import (
    A2aAgentExecutor as UpstreamA2aAgentExecutor,
)
from google.adk.a2a.executor.a2a_agent_executor import (
    A2aAgentExecutorConfig as UpstreamA2aAgentExecutorConfig,
)
from google.adk.events import Event, EventActions
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
from google.adk.runners import Runner
from google.adk.sessions import Session
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.adk.utils.context_utils import Aclosing
from google.genai import types as genai_types
from pydantic import BaseModel
from typing_extensions import override

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    TaskResultAggregator,
    extract_ask_user_answers_from_message,
    extract_batch_decisions_from_message,
    extract_decision_from_message,
    extract_rejection_reasons_from_message,
    get_kagent_metadata_key,
)
from kagent.core.tracing._span_processor import (
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)

from ._mcp_toolset import is_anyio_cross_task_cancel_scope_error
from .types import RetryPolicyConfig
from ._remote_a2a_tool import SubagentSessionProvider
from .converters.event_converter import convert_event_to_a2a_events, serialize_metadata_value
from .converters.part_converter import convert_a2a_part_to_genai_part, convert_genai_part_to_a2a_part
from .converters.request_converter import convert_a2a_request_to_adk_run_args

logger = logging.getLogger("kagent_adk." + __name__)


class A2aAgentExecutorConfig(BaseModel):
    """Configuration for the KAgent A2aAgentExecutor."""

    stream: bool = False
    retry_policy: Optional["RetryPolicyConfig"] = None


def _compute_retry_delay(attempt: int, policy: "RetryPolicyConfig") -> float:
    """Compute retry delay using exponential backoff."""
    delay = policy.initial_retry_delay_seconds * (2**attempt)
    if policy.max_retry_delay_seconds is not None:
        delay = min(delay, policy.max_retry_delay_seconds)
    return delay


def _kagent_request_converter(request, _part_converter=None):
    """Adapter to match the upstream A2ARequestToAgentRunRequestConverter signature.

    Upstream expects (RequestContext, A2APartToGenAIPartConverter) -> AgentRunRequest.
    Kagent's converter has a different signature, so this wraps it to satisfy
    the upstream config type while still using kagent's own conversion logic.
    """
    from google.adk.a2a.converters.request_converter import AgentRunRequest

    run_args = convert_a2a_request_to_adk_run_args(request, stream=False)
    return AgentRunRequest(
        user_id=run_args["user_id"],
        session_id=run_args["session_id"],
        new_message=run_args["new_message"],
        run_config=run_args["run_config"],
    )


def _kagent_event_converter(event, invocation_context, task_id=None, context_id=None, _part_converter=None):
    """Adapter to match the upstream AdkEventToA2AEventsConverter signature.

    Upstream expects (Event, InvocationContext, task_id, context_id, GenAIPartToA2APartConverter).
    Kagent's converter doesn't take a part_converter arg, so this wraps it.
    """
    return convert_event_to_a2a_events(event, invocation_context, task_id, context_id)


class A2aAgentExecutor(UpstreamA2aAgentExecutor):
    """KAgent's A2A agent executor.

    Extends the upstream google-adk A2aAgentExecutor with:
    - Per-request runner lifecycle (created fresh and closed after each request)
    - OpenTelemetry span attribute management
    - Enhanced error handling (Ollama-specific JSON parse errors, CancelledError)
    - Partial event filtering to avoid duplicate aggregation during streaming
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
        # Build upstream config with kagent's custom converters
        upstream_config = UpstreamA2aAgentExecutorConfig(
            a2a_part_converter=convert_a2a_part_to_genai_part,
            gen_ai_part_converter=convert_genai_part_to_a2a_part,
            request_converter=_kagent_request_converter,
            event_converter=_kagent_event_converter,
        )
        super().__init__(runner=runner, config=upstream_config)
        self._kagent_config = config
        self._task_store = task_store

    @override
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

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not supported")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        """Executes an A2A request with optional retry on failure."""
        retry_policy = self._kagent_config.retry_policy if self._kagent_config else None
        max_attempts = 1 + (retry_policy.max_retries if retry_policy else 0)
        last_error: Exception | None = None

        for attempt in range(max_attempts):
            try:
                await self._execute_impl(context, event_queue)
                return  # success
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
                # Never retry cancellations.
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
                return
            except Exception as e:
                last_error = e
                if attempt + 1 < max_attempts:
                    delay = _compute_retry_delay(attempt, retry_policy)
                    logger.warning(
                        "Request failed (attempt %d/%d), retrying in %.1fs: %s",
                        attempt + 1,
                        max_attempts,
                        delay,
                        e,
                    )
                    await asyncio.sleep(delay)

        # All attempts exhausted — publish failure (matches original behavior)
        if last_error is not None:
            logger.error("All %d attempts failed: %s", max_attempts, last_error, exc_info=True)
            error_message = str(last_error)
            await self._publish_failed_status_event(context, event_queue, error_message)

    async def _execute_impl(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
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
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.submitted,
                            message=context.message,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                        ),
                        context_id=context.context_id,
                        final=False,
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
                        state=TaskState.failed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=error_message))],
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
        except BaseException as enqueue_error:
            if isinstance(enqueue_error, (KeyboardInterrupt, SystemExit)):
                raise
            logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)

    @staticmethod
    def _find_pending_confirmations(session: Session) -> dict[str, tuple[str | None, dict | None]]:
        """Find pending adk_request_confirmation calls and their original tool call IDs.

        Scans session events backwards for the most recent adk_request_confirmation
        FunctionCall events that haven't been responded to yet.

        Returns:
            Dict mapping confirmation function_call_id to a tuple of:
              - the original tool call ID (from args.originalFunctionCall.id), or None
              - the original toolConfirmation payload (from args.toolConfirmation.payload), or None
        """
        pending: dict[str, tuple[str | None, dict | None]] = {}
        responded_ids: set[str] = set()

        for event in reversed(session.events or []):
            # Collect responded confirmation IDs
            for fr in event.get_function_responses():
                if fr.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME and fr.id is not None:
                    responded_ids.add(fr.id)

            # Collect requested confirmation IDs and extract original tool call ID + payload
            for fc in event.get_function_calls():
                if fc.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME and fc.id is not None:
                    original_id = None
                    original_payload = None
                    if fc.args and isinstance(fc.args, dict):
                        orig_fc = fc.args.get("originalFunctionCall")
                        if isinstance(orig_fc, dict):
                            original_id = orig_fc.get("id")
                        tool_conf = fc.args.get("toolConfirmation")
                        if isinstance(tool_conf, dict):
                            original_payload = tool_conf.get("payload")
                            if isinstance(original_payload, dict):
                                original_payload = dict(original_payload)
                            else:
                                original_payload = None
                    pending[fc.id] = (original_id, original_payload)

            # Stop scanning once we find confirmation requests (they're recent)
            if pending:
                break

        # Remove the ones that have already been responded to
        for responded_id in responded_ids:
            pending.pop(responded_id, None)

        return pending

    @staticmethod
    def _build_confirmation_payload(
        original_payload: dict | None,
        extra: dict | None,
    ) -> dict | None:
        """Merge the original request_confirmation payload with decision-specific data.

        The original payload (set by the tool in ``request_confirmation()``) is
        preserved so that the tool's ``_handle_resume`` can read its own state
        (e.g. subagent task_id, context_id).  Decision-specific keys (like
        ``rejection_reason``) are merged on top.
        """
        if not original_payload and not extra:
            return None
        merged: dict = {}
        if original_payload:
            merged.update(original_payload)
        if extra:
            merged.update(extra)
        return merged

    def _process_hitl_decision(
        self, session: Session, decision: str, message: Message
    ) -> list[genai_types.Part] | None:
        """Process a HITL decision from a message and return the corresponding FunctionResponse parts."""
        pending_confirmations = self._find_pending_confirmations(session)
        if not pending_confirmations:
            return None

        logger.info(
            "HITL continuation: decision=%s, pending=%s",
            decision,
            {fc_id: orig_id for fc_id, (orig_id, _) in pending_confirmations.items()},
        )

        # Check for ask-user answers — if present, build a single approved
        # ToolConfirmation with the answers payload regardless of decision_type.
        # The tool will use the payload and construct the user answer to the agent
        ask_user_answers = extract_ask_user_answers_from_message(message)
        if ask_user_answers is not None:
            parts = []
            for fc_id, (_, orig_payload) in pending_confirmations.items():
                payload = self._build_confirmation_payload(orig_payload, {"answers": ask_user_answers})
                confirmation = ToolConfirmation(confirmed=True, payload=payload)
                parts.append(
                    genai_types.Part(
                        function_response=genai_types.FunctionResponse(
                            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                            id=fc_id,
                            response={"response": confirmation.model_dump_json()},
                        )
                    )
                )
            return parts

        # Extract optional rejection reasons from the message.
        rejection_reasons = extract_rejection_reasons_from_message(message)

        if decision == KAGENT_HITL_DECISION_TYPE_BATCH:
            # Batch mode: per-tool decisions
            batch_decisions = extract_batch_decisions_from_message(message) or {}
            logger.info(
                "HITL batch: batch_decisions=%s, rejection_reasons=%s",
                batch_decisions,
                rejection_reasons,
            )
            parts = []
            for fc_id, (original_id, orig_payload) in pending_confirmations.items():
                # Check if this is a subagent HITL request by checking if orig_payload has hitl_parts.
                is_subagent = bool(orig_payload and orig_payload.get("hitl_parts"))

                if is_subagent:
                    # Forward the entire batch decision to the tool so
                    # _handle_resume can relay it to the subagent as-is.
                    all_approved = all(d == KAGENT_HITL_DECISION_TYPE_APPROVE for d in batch_decisions.values())
                    extra: dict = {"batch_decisions": batch_decisions}
                    if rejection_reasons:
                        extra["rejection_reasons"] = rejection_reasons
                    payload = self._build_confirmation_payload(orig_payload, extra)
                    confirmation = ToolConfirmation(confirmed=all_approved, payload=payload)
                else:
                    # Direct tool — look up by original_id as before
                    tool_decision = batch_decisions.get(original_id, KAGENT_HITL_DECISION_TYPE_APPROVE)
                    confirmed = tool_decision == KAGENT_HITL_DECISION_TYPE_APPROVE
                    extra_reject: dict | None = None
                    if not confirmed and rejection_reasons:
                        reason = rejection_reasons.get(original_id) if original_id else None
                        if reason:
                            extra_reject = {"rejection_reason": reason}
                    payload = self._build_confirmation_payload(orig_payload, extra_reject)
                    confirmation = ToolConfirmation(confirmed=confirmed, payload=payload)

                parts.append(
                    genai_types.Part(
                        function_response=genai_types.FunctionResponse(
                            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                            id=fc_id,
                            response={"response": confirmation.model_dump_json()},
                        )
                    )
                )
            return parts
        else:
            # Uniform mode: same decision for all pending tools
            confirmed = decision == KAGENT_HITL_DECISION_TYPE_APPROVE
            # Attach rejection reason if provided (uniform denial uses "*" sentinel)
            uniform_extra: dict | None = None
            if not confirmed and rejection_reasons:
                reason = rejection_reasons.get("*")
                if reason:
                    uniform_extra = {"rejection_reason": reason}
            parts = []
            for fc_id, (_, orig_payload) in pending_confirmations.items():
                merged_payload = self._build_confirmation_payload(orig_payload, uniform_extra)
                confirmation = ToolConfirmation(confirmed=confirmed, payload=merged_payload)
                serialized = confirmation.model_dump_json()
                parts.append(
                    genai_types.Part(
                        function_response=genai_types.FunctionResponse(
                            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                            id=fc_id,
                            response={"response": serialized},
                        )
                    )
                )
            return parts

    async def _handle_request(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        runner: Runner,
        run_args: dict[str, Any],
    ):
        # ensure the session exists
        session = await self._prepare_session(context, run_args, runner)

        # HITL resume: translate A2A approval/rejection to ADK FunctionResponse
        decision = extract_decision_from_message(context.message)
        if decision:
            parts = self._process_hitl_decision(session, decision, context.message)
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
                    state=TaskState.working,
                    timestamp=datetime.now(timezone.utc).isoformat(),
                ),
                context_id=context.context_id,
                final=False,
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

        task_result_aggregator = TaskResultAggregator()
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

                for a2a_event in convert_event_to_a2a_events(
                    adk_event,
                    invocation_context,
                    context.task_id,
                    context.context_id,
                    subagent_session_ids=subagent_session_ids or None,
                ):
                    # Only aggregate non-partial events to avoid duplicates from streaming chunks
                    # Partial events are sent to frontend for display but not accumulated
                    if not adk_event.partial:
                        task_result_aggregator.process_event(a2a_event)
                    await event_queue.enqueue_event(a2a_event)

                # Break on confirmation events that use long running tools
                if getattr(adk_event, "long_running_tool_ids", None):
                    break

        # Attach the last LLM usage to run_metadata so the A2A task_manager
        # merges it into task.metadata on the completed Task object.
        if last_usage_metadata is not None:
            run_metadata[get_kagent_metadata_key("usage_metadata")] = serialize_metadata_value(last_usage_metadata)

        # publish the task result event - this is final
        if (
            task_result_aggregator.task_state == TaskState.working
            and task_result_aggregator.task_status_message is not None
            and task_result_aggregator.task_status_message.parts
        ):
            # if task is still working properly, publish the artifact update event as
            # the final result according to a2a protocol.
            await event_queue.enqueue_event(
                TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    last_chunk=True,
                    context_id=context.context_id,
                    artifact=Artifact(
                        artifact_id=str(uuid.uuid4()),
                        parts=task_result_aggregator.task_status_message.parts,
                    ),
                )
            )
            # publish the final status update event
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.completed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=True,
                    metadata=run_metadata,
                )
            )
        else:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=task_result_aggregator.task_state,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=task_result_aggregator.task_status_message,
                    ),
                    context_id=context.context_id,
                    final=True,
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
                    # A2A parts have a .root property that contains the actual part (TextPart, FilePart, etc.)
                    if isinstance(part, Part):
                        root_part = part.root
                        if isinstance(root_part, TextPart) and root_part.text:
                            # Take first 20 chars + "..." if longer (matching UI behavior)
                            text = root_part.text.strip()
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
