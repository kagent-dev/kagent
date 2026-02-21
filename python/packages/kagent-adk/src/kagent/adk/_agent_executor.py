from __future__ import annotations

import asyncio
import inspect
import logging
import os
import signal
import uuid
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
from google.adk.runners import Runner
from google.adk.utils.context_utils import Aclosing
from pydantic import BaseModel
from typing_extensions import override

from kagent.core.a2a import TaskResultAggregator, get_kagent_metadata_key
from kagent.core.tracing._span_processor import (
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)

from .converters.event_converter import convert_event_to_a2a_events
from .converters.part_converter import convert_a2a_part_to_genai_part, convert_genai_part_to_a2a_part
from .converters.request_converter import convert_a2a_request_to_adk_run_args

logger = logging.getLogger("kagent_adk." + __name__)


async def _force_close_mcp_sessions(runner: Runner) -> None:
    """Force-close orphaned MCP sessions when graceful cleanup fails.

    When Runner.close() fails due to anyio cancel scope task context
    mismatches (common with MCP stdio sessions created in a different
    asyncio task), this function directly closes the session exit stacks
    and terminates any orphaned child processes.

    See: https://github.com/kagent-dev/kagent/issues/1073
    See: https://github.com/modelcontextprotocol/python-sdk/issues/521
    """
    try:
        toolsets = runner._collect_toolset(runner.agent)
    except Exception as e:
        logger.warning("Failed to collect toolsets for force cleanup: %s", e)
        logger.warning(
            "Falling back to last-resort child process cleanup"
        )
        _terminate_mcp_child_processes()
        return

    for toolset in toolsets:
        mgr = getattr(toolset, "_mcp_session_manager", None)
        if mgr is None:
            continue

        sessions = getattr(mgr, "_sessions", None)
        if not sessions:
            continue

        for session_key in list(sessions.keys()):
            try:
                _client_session, exit_stack, _stored_loop = sessions[session_key]
            except (ValueError, KeyError):
                continue

            # Attempt to close the AsyncExitStack which manages the
            # stdio_client context (and its subprocess).
            closed = False
            try:
                await asyncio.wait_for(exit_stack.aclose(), timeout=5.0)
                logger.info(
                    "Force-closed MCP session exit stack: %s", session_key
                )
                closed = True
            except Exception as close_err:
                logger.warning(
                    "Exit stack close failed for %s (%s: %s), "
                    "terminating MCP child processes as last resort",
                    session_key,
                    type(close_err).__name__,
                    close_err,
                )
                # Last resort: terminate MCP stdio child processes
                _terminate_mcp_child_processes()

            # Only remove the session if cleanup succeeded to allow
            # potential future retry if the process wasn't terminated
            if closed:
                sessions.pop(session_key, None)


def _is_mcp_stdio_process(pid: int) -> bool:
    """Check if a process is an MCP stdio server by inspecting its command line.

    Returns True if the process command line contains indicators of being
    an MCP server process (e.g., 'mcp', 'npx', 'uvx' with server args).
    """
    try:
        cmdline_path = f"/proc/{pid}/cmdline"
        with open(cmdline_path, "rb") as f:
            cmdline = f.read().decode("utf-8", errors="replace")
        # cmdline fields are null-separated
        cmdline_lower = cmdline.lower()
        # Match common MCP stdio server patterns:
        # - commands containing "mcp" (mcp server packages, @modelcontextprotocol/*)
        # - npx/uvx invocations (common MCP stdio launchers)
        # - python -m mcp or similar
        mcp_indicators = ["mcp", "npx", "uvx", "modelcontextprotocol"]
        return any(indicator in cmdline_lower for indicator in mcp_indicators)
    except (OSError, ValueError):
        return False


def _terminate_mcp_child_processes() -> None:
    """Terminate orphaned MCP stdio child processes of the current process.

    This is a last-resort cleanup for orphaned MCP stdio subprocesses
    that could not be terminated through normal exit stack cleanup.
    Only targets direct children whose command line matches MCP stdio
    server patterns to minimize blast radius.

    Sends SIGTERM first, then SIGKILL after a short grace period for
    processes that don't exit.
    """
    pid = os.getpid()
    terminated_pids: list[int] = []
    try:
        # Read /proc to find direct children of this process
        for entry in os.listdir("/proc"):
            if not entry.isdigit():
                continue
            try:
                stat_path = f"/proc/{entry}/stat"
                with open(stat_path, "r") as f:
                    stat = f.read()
                # Field 4 (0-indexed: 3) is the parent PID
                fields = stat.split()
                if len(fields) > 3 and int(fields[3]) == pid:
                    child_pid = int(entry)
                    if not _is_mcp_stdio_process(child_pid):
                        continue
                    os.kill(child_pid, signal.SIGTERM)
                    terminated_pids.append(child_pid)
                    logger.info(
                        "Sent SIGTERM to orphaned MCP child process PID %d",
                        child_pid,
                    )
            except (OSError, ValueError, IndexError):
                continue
    except OSError:
        # /proc not available (non-Linux), skip process cleanup
        return

    if not terminated_pids:
        return

    # Give processes a short grace period then SIGKILL any survivors
    import time

    time.sleep(0.5)
    for child_pid in terminated_pids:
        try:
            # Check if process still exists (signal 0 = check only)
            os.kill(child_pid, 0)
            # Still alive — force kill
            os.kill(child_pid, signal.SIGKILL)
            logger.info(
                "Sent SIGKILL to MCP child process PID %d (did not exit after SIGTERM)",
                child_pid,
            )
        except OSError:
            # Process already exited, good
            pass


class A2aAgentExecutorConfig(BaseModel):
    """Configuration for the KAgent A2aAgentExecutor."""

    stream: bool = False


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
        """Executes an A2A request and publishes updates to the event queue
        specified. It runs as following:
        * Takes the input from the A2A request
        * Convert the input to ADK input content, and runs the ADK agent
        * Collects output events of the underlying ADK Agent
        * Converts the ADK output events into A2A task updates
        * Publishes the updates back to A2A server via event queue
        """
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
            # Close the runner which cleans up MCP toolsets.
            # Since the runner is created per A2A request and MCP toolsets
            # are not shared between requests, cleanup is mandatory.
            #
            # The MCP Python SDK uses anyio cancel scopes that must be
            # entered and exited in the same asyncio task context. When
            # cleanup runs in a different task than the one that created
            # the sessions, a RuntimeError is raised and MCP stdio
            # subprocesses are left orphaned, causing high CPU at idle.
            # See: https://github.com/kagent-dev/kagent/issues/1073
            if runner is not None:
                try:
                    await runner.close()
                except asyncio.CancelledError as e:
                    logger.warning(
                        "Runner.close() was cancelled, "
                        "forcing MCP session cleanup",
                    )
                    await _force_close_mcp_sessions(runner)
                    # Re-raise so upstream cancellation handling still works
                    raise
                except RuntimeError as e:
                    # Only suppress the specific anyio cancel-scope mismatch
                    # error; re-raise unexpected RuntimeErrors after cleanup.
                    is_cancel_scope_error = "cancel scope" in str(e).lower()
                    if is_cancel_scope_error:
                        logger.warning(
                            "Runner.close() hit cancel-scope mismatch (%s), "
                            "forcing MCP session cleanup",
                            e,
                        )
                    else:
                        logger.error(
                            "Unexpected RuntimeError during runner cleanup: %s",
                            e,
                            exc_info=True,
                        )
                    await _force_close_mcp_sessions(runner)
                    if not is_cancel_scope_error:
                        raise
                except Exception as e:
                    logger.error(
                        "Unexpected error during runner cleanup: %s",
                        e,
                        exc_info=True,
                    )
                    await _force_close_mcp_sessions(runner)

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
        except Exception as enqueue_error:
            logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)

    async def _handle_request(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        runner: Runner,
        run_args: dict[str, Any],
    ):
        # ensure the session exists
        session = await self._prepare_session(context, run_args, runner)

        # set request headers to session state
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

        task_result_aggregator = TaskResultAggregator()
        async with Aclosing(runner.run_async(**run_args)) as agen:
            async for adk_event in agen:
                # Capture the real invocation_id from the first ADK event that has one
                event_inv_id = getattr(adk_event, "invocation_id", None)
                if event_inv_id and not real_invocation_id:
                    real_invocation_id = event_inv_id
                    run_metadata[get_kagent_metadata_key("invocation_id")] = real_invocation_id
                for a2a_event in convert_event_to_a2a_events(
                    adk_event, invocation_context, context.task_id, context.context_id
                ):
                    # Only aggregate non-partial events to avoid duplicates from streaming chunks
                    # Partial events are sent to frontend for display but not accumulated
                    if not adk_event.partial:
                        task_result_aggregator.process_event(a2a_event)
                    await event_queue.enqueue_event(a2a_event)

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

            session = await runner.session_service.create_session(
                app_name=runner.app_name,
                user_id=user_id,
                state={"session_name": session_name},
                session_id=session_id,
            )

            # Update run_args with the new session_id
            run_args["session_id"] = session.id

        return session
