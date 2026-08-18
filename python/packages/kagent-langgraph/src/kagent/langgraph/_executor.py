"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import asyncio
import logging
import uuid
from collections.abc import Mapping
from typing import Any

try:
    from typing import override  # Python 3.12+
except ImportError:
    from typing_extensions import override

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Message,
    Part,
    Role,
    Task,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    HitlTool,
    ToolApprovalRequest,
    attach_hitl_extension,
    get_ask_user_request,
    get_ask_user_response,
    get_hitl_payload,
    get_kagent_metadata_key,
    get_tool_approval_request,
    get_tool_approval_response,
    hitl_activated,
    hitl_fallback_text,
    now_timestamp,
    require_ask_user_response,
    require_tool_approval_response,
)
from kagent.core.tracing._span_processor import (
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel

from langgraph.graph.state import CompiledStateGraph
from langgraph.types import Command

from ._converters import _convert_langgraph_event_to_a2a
from ._error_mappings import get_error_metadata, get_user_friendly_error_message

logger = logging.getLogger(__name__)


class LangGraphAgentExecutorConfig(BaseModel):
    """Configuration for the LangGraphAgentExecutor."""

    # Maximum time to wait for graph execution (seconds)
    execution_timeout: float = 300.0

    # Whether to stream intermediate results
    enable_streaming: bool = True


class LangGraphAgentExecutor(AgentExecutor):
    """An AgentExecutor that runs LangGraph workflows against A2A requests.

    This executor integrates LangGraph with the A2A protocol, handling session
    management, event streaming, and result aggregation.
    """

    def __init__(
        self,
        *,
        graph: CompiledStateGraph,
        app_name: str,
        config: LangGraphAgentExecutorConfig | None = None,
    ):
        """Initialize the executor.

        Args:
            graph: Compiled LangGraph
            app_name: Application name for session management
            config: Optional executor configuration
        """
        super().__init__()
        self._graph = graph
        self.app_name = app_name
        self._config = config or LangGraphAgentExecutorConfig()

    def _create_graph_config(self, context: RequestContext) -> RunnableConfig:
        """Create LangGraph config from A2A request context."""
        # Extract session information
        session_id = getattr(context, "session_id", None) or context.context_id
        span_attributes = _convert_a2a_request_to_span_attributes(context)

        return {
            "configurable": {
                "thread_id": session_id,
                "app_name": self.app_name,
            },
            "project_name": self.app_name,
            "run_name": "kagent-langgraph-exec",
            "tags": [
                "kagent",
                "langgraph",
                f"app:{self.app_name}",
                f"task:{context.task_id}",
                f"context:{context.context_id}",
                f"session:{session_id}",
            ],
            "metadata": {
                "kagent_app_name": self.app_name,
                "a2a_context_id": context.context_id,
                "a2a_task_id": context.task_id,
                "a2a_request_id": getattr(context, "request_id", None),
                **span_attributes,
            },
        }

    async def _stream_graph_events(
        self,
        graph: CompiledStateGraph,
        input_data: dict[str, Any],
        config: RunnableConfig,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Stream LangGraph events and convert them to A2A events."""
        # Track final state for interrupt detection
        final_state: dict[str, Any] | None = None

        # Track message IDs we've already sent to avoid duplicates
        sent_message_ids: set[str] = set()

        # Stream events from the graph
        async for event in graph.astream(
            input_data,
            config,
            stream_mode="updates",
        ):
            # Store final state
            final_state = event

            # Convert LangGraph events to A2A events
            a2a_events = await _convert_langgraph_event_to_a2a(
                event, context.task_id, context.context_id, self.app_name, sent_message_ids
            )
            for a2a_event in a2a_events:
                await event_queue.enqueue_event(a2a_event)

        # Check for interrupts after streaming completes
        if final_state and final_state.get("__interrupt__"):
            interrupt_data = final_state["__interrupt__"]
            headers = _call_state(context).get("headers")
            await self._handle_interrupt(
                interrupt_data=interrupt_data,
                task_id=context.task_id,
                context_id=context.context_id,
                event_queue=event_queue,
                hitl_enabled=hitl_activated(headers if isinstance(headers, Mapping) else {}),
            )
            # Interrupt detected - input_required event already sent, so return early
            return

        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.TASK_STATE_COMPLETED,
                    timestamp=now_timestamp(),
                ),
                context_id=context.context_id,
            )
        )

    async def _handle_interrupt(
        self,
        interrupt_data: list[Any],
        task_id: str,
        context_id: str,
        event_queue: EventQueue,
        hitl_enabled: bool,
    ) -> None:
        """Handle interrupt from LangGraph and convert to A2A input_required event.

        BYO graphs call ``interrupt()`` with ``action_requests``: tool calls that
        need approval. ``action_requests[].id`` is the HITL correlation id (and
        usually the tool call id); the same id comes back in the resume value's
        ``tool_approval_response.approvals[].id``.
        """
        if not interrupt_data:
            logger.warning("Empty interrupt data received")
            return

        # Safely extract interrupt value (LangGraph-specific format)
        first_item = interrupt_data[0]
        if hasattr(first_item, "value"):
            interrupt_value = first_item.value
        elif isinstance(first_item, dict):
            interrupt_value = first_item
        else:
            logger.error(f"Unexpected interrupt data type: {type(first_item)}")
            return

        action_requests_raw = interrupt_value.get("action_requests", [])
        if not action_requests_raw:
            logger.warning("Interrupt has no action_requests, ignoring")
            return

        tools: list[HitlTool] = []
        for action in action_requests_raw:
            if not isinstance(action, Mapping):
                logger.warning(
                    "Skipping malformed action_request entry of type %s: %r",
                    type(action),
                    action,
                )
                continue
            tool_name = action["name"]
            tool_args = action["args"]
            # id is the opaque HITL correlation id; call_id is the tool call id.
            # Graphs typically set both to the LangChain tool call id.
            correlation_id = action["id"]
            call_id = action.get("call_id") or correlation_id
            tools.append(HitlTool(id=correlation_id, call_id=call_id, name=tool_name, args=tool_args))

        # The text part names the tools so a client that did not activate the
        # extension still learns what it is being asked.
        text = hitl_fallback_text(tools)
        status_message = Message(
            message_id=str(uuid.uuid4()),
            role=Role.ROLE_AGENT,
            task_id=task_id,
            context_id=context_id,
            parts=[Part(text=text)],
        )
        if hitl_enabled:
            attach_hitl_extension(status_message, ToolApprovalRequest(hint=text, tools=tools))

        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=task_id,
                status=TaskStatus(
                    state=TaskState.TASK_STATE_INPUT_REQUIRED,
                    timestamp=now_timestamp(),
                    message=status_message,
                ),
                context_id=context_id,
            )
        )

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not implemented")

    def _is_resume_command(self, context: RequestContext) -> bool:
        """Check if message is a resume command for an interrupted task."""
        # Must have an existing task in input_required state to resume
        if not context.current_task:
            return False

        if context.current_task.status.state != TaskState.TASK_STATE_INPUT_REQUIRED:
            return False

        # Route even malformed or wrong-type HITL responses through validation.
        return get_hitl_payload(context.message) is not None

    async def _handle_resume(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Resume graph execution after interrupt with the hitl/v1 response payload."""
        approval_response = get_tool_approval_response(context.message)
        ask_response = get_ask_user_response(context.message)
        stored_message = context.current_task.status.message if context.current_task else None
        tool_request = get_tool_approval_request(stored_message)
        ask_request = get_ask_user_request(stored_message)

        if tool_request is not None:
            resume_value = require_tool_approval_response(tool_request, approval_response).model_dump(exclude_none=True)
        elif ask_request is not None:
            resume_value = require_ask_user_response(ask_request, ask_response).model_dump(exclude_none=True)
        else:
            raise ValueError("Stored input-required task has no HITL request")

        # Task.metadata is a protobuf Struct, not a dict.
        task_metadata = _task_metadata(context.current_task)
        thread_id = task_metadata.get(get_kagent_metadata_key("thread_id")) or task_metadata.get("thread_id")
        if not thread_id:
            # Fallback to computing from context (same as initial)
            thread_id = getattr(context, "session_id", None) or context.context_id

        logger.info(
            "Resuming after interrupt - task_id=%s, thread_id=%s, type=%s",
            context.task_id,
            thread_id,
            resume_value.get("type"),
        )

        resume_input = Command(resume=resume_value)
        span_attributes = _convert_a2a_request_to_span_attributes(context)

        # Create graph config with explicit thread_id
        config = {
            "configurable": {
                "thread_id": thread_id,  # Use thread from interrupted task!
                "app_name": self.app_name,
            },
            "project_name": self.app_name,
            "run_name": "kagent-langgraph-resume",
            "tags": [
                "kagent",
                "langgraph",
                "resume",
                f"app:{self.app_name}",
                f"task:{context.task_id}",
                f"context:{context.context_id}",
                f"thread:{thread_id}",
            ],
            "metadata": {
                "kagent_app_name": self.app_name,
                "a2a_context_id": context.context_id,
                "a2a_task_id": context.task_id,
                "thread_id": thread_id,
                "resume": True,
                **span_attributes,
            },
        }

        # Send working status
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.TASK_STATE_WORKING,
                    timestamp=now_timestamp(),
                ),
                context_id=context.context_id,
            )
        )

        # Resume graph execution
        try:
            await asyncio.wait_for(
                self._stream_graph_events(
                    self._graph,
                    resume_input,  # Pass Command instead of messages
                    config,
                    context,
                    event_queue,
                ),
                timeout=self._config.execution_timeout,
            )
        except Exception as e:
            logger.error(f"Error during resume: {e}", exc_info=True)
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.TASK_STATE_FAILED,
                        timestamp=now_timestamp(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.ROLE_AGENT,
                            parts=[Part(text=f"Resume failed: {str(e)}")],
                        ),
                    ),
                    context_id=context.context_id,
                )
            )

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        """Execute the LangGraph workflow and publish updates to the event queue."""
        if not context.message:
            raise ValueError("A2A request must have a message")

        # Convert the a2a request to kagent span attributes.
        span_attributes = _convert_a2a_request_to_span_attributes(context)

        # Set kagent span attributes for all spans in context.
        context_token = set_kagent_span_attributes(span_attributes)
        try:
            # Check if this is a resume command (check before current_task check)
            # Resume commands can come as new messages to continue interrupted tasks
            if self._is_resume_command(context):
                logger.info(f"Resuming task {context.task_id} after interrupt")
                await self._handle_resume(context, event_queue)
                return

            # For new tasks, the first event must be a Task (not TaskStatusUpdateEvent).
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

            # Calculate and store thread_id for potential resume
            thread_id = getattr(context, "session_id", None) or context.context_id

            # Send working status
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.TASK_STATE_WORKING,
                        timestamp=now_timestamp(),
                    ),
                    context_id=context.context_id,
                    metadata={
                        get_kagent_metadata_key("app_name"): self.app_name,
                        get_kagent_metadata_key("session_id"): getattr(context, "session_id", context.context_id),
                        get_kagent_metadata_key("thread_id"): thread_id,
                    },
                )
            )

            try:
                # Resolve the graph

                # Convert A2A message to LangChain format
                inputs = {"messages": [("user", context.get_user_input())]}

                # Create graph config
                config = self._create_graph_config(context)

                # Stream graph execution
                await asyncio.wait_for(
                    self._stream_graph_events(self._graph, inputs, config, context, event_queue),
                    timeout=self._config.execution_timeout,
                )

            except TimeoutError:
                logger.error(f"Graph execution timed out after {self._config.execution_timeout} seconds")
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.TASK_STATE_FAILED,
                            timestamp=now_timestamp(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.ROLE_AGENT,
                                parts=[Part(text="Execution timed out")],
                            ),
                        ),
                        context_id=context.context_id,
                    )
                )
            except Exception as e:
                logger.error(f"Error during LangGraph execution: {e}", exc_info=True)

                # Get user-friendly message
                user_message = get_user_friendly_error_message(e)
                error_meta = get_error_metadata(e)

                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.TASK_STATE_FAILED,
                            timestamp=now_timestamp(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.ROLE_AGENT,
                                parts=[Part(text=user_message)],
                                metadata={
                                    get_kagent_metadata_key("error_type"): error_meta["error_type"],
                                    get_kagent_metadata_key("error_detail"): error_meta["error_detail"],
                                },
                            ),
                        ),
                        context_id=context.context_id,
                        metadata={
                            get_kagent_metadata_key("error_type"): error_meta["error_type"],
                            get_kagent_metadata_key("error_detail"): error_meta["error_detail"],
                        },
                    )
                )
        finally:
            clear_kagent_span_attributes(context_token)


def _get_user_id(request: RequestContext) -> str:
    # Get user from call context if available (auth is enabled on a2a server)
    if request.call_context and request.call_context.user and request.call_context.user.user_name:
        return request.call_context.user.user_name

    # Get user from context id
    return f"A2A_USER_{request.context_id}"


def _call_state(context: RequestContext) -> dict[str, Any]:
    state = getattr(context.call_context, "state", None)
    return state if isinstance(state, dict) else {}


def _task_metadata(task: Task | None) -> dict[str, Any]:
    """Return task metadata as a plain dict (A2A Task.metadata is a Struct)."""
    if task is None or not task.HasField("metadata"):
        return {}
    return MessageToDict(task.metadata)


def _convert_a2a_request_to_span_attributes(
    request: RequestContext,
) -> dict[str, Any]:
    if not request.message:
        raise ValueError("Request message cannot be None")

    span_attributes = {
        "kagent.user_id": _get_user_id(request),
        "gen_ai.conversation.id": request.context_id,
    }

    if request.task_id:
        span_attributes["gen_ai.task.id"] = request.task_id

    return span_attributes
