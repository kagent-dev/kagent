"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import asyncio
import logging
import uuid
from datetime import datetime
from typing import Any

try:
    from datetime import UTC  # Python 3.11+
except ImportError:
    from datetime import timezone

    UTC = timezone.utc

try:
    from typing import override  # Python 3.12+
except ImportError:
    from typing_extensions import override

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    DataPart,
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel

from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_REJECT,
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
        task_result_aggregator = TaskResultAggregator()

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
                task_result_aggregator.process_event(a2a_event)
                await event_queue.enqueue_event(a2a_event)

        # Check for interrupts after streaming completes
        if final_state and final_state.get("__interrupt__"):
            interrupt_data = final_state["__interrupt__"]
            await self._handle_interrupt(
                interrupt_data=interrupt_data,
                task_id=context.task_id,
                context_id=context.context_id,
                event_queue=event_queue,
            )
            # Interrupt detected - input_required event already sent, so return early
            return

        # Final artifacts are already sent through individual event processing

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
            # public the final status update event
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.completed,
                        timestamp=datetime.now(UTC).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
        else:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=task_result_aggregator.task_state,
                        timestamp=datetime.now(UTC).isoformat(),
                        message=task_result_aggregator.task_status_message,
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )

    async def _handle_interrupt(
        self,
        interrupt_data: list[Any],
        task_id: str,
        context_id: str,
        event_queue: EventQueue,
    ) -> None:
        """Handle interrupt from LangGraph and convert to A2A input_required event.

        The BYO graph is expected to call ``interrupt()`` with a dict containing
        ``action_requests`` -- a list of tool calls that need approval.

        This method converts them into ``DataPart`` objects with the same
        ``adk_request_confirmation`` shape the ADK executor emits, so the
        frontend can render them identically.
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

        # Build DataParts in the adk_request_confirmation wire format so the
        # frontend renders tool-approval cards identically to the ADK executor.
        parts: list[Part] = []
        for action in action_requests_raw:
            tool_name = action.get("name", "unknown")
            tool_args = action.get("args", {})
            tool_call_id = action.get("id")
            confirmation_id = str(uuid.uuid4())

            parts.append(
                Part(
                    DataPart(
                        data={
                            "name": "adk_request_confirmation",
                            "id": confirmation_id,
                            "args": {
                                "originalFunctionCall": {
                                    "name": tool_name,
                                    "args": tool_args,
                                    "id": tool_call_id,
                                },
                                "toolConfirmation": {
                                    "hint": f"Tool '{tool_name}' requires approval before execution.",
                                    "confirmed": False,
                                    "payload": None,
                                },
                            },
                        },
                        metadata={
                            get_kagent_metadata_key(
                                A2A_DATA_PART_METADATA_TYPE_KEY
                            ): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
                            get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY): True,
                        },
                    )
                )
            )

        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=task_id,
                status=TaskStatus(
                    state=TaskState.input_required,
                    timestamp=datetime.now(UTC).isoformat(),
                    message=Message(
                        message_id=str(uuid.uuid4()),
                        role=Role.agent,
                        parts=parts,
                    ),
                ),
                context_id=context_id,
                final=False,
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

        if context.current_task.status.state != TaskState.input_required:
            return False

        # Check if message contains a decision
        decision = extract_decision_from_message(context.message)
        return decision is not None

    async def _handle_resume(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Resume graph execution after interrupt with user decision.

        Extracts the full HITL decision payload from the A2A message and
        forwards it to the graph via ``Command(resume=...)``.  The resume
        value includes:

        - ``decision_type``: ``"approve"``, ``"reject"``, or ``"batch"``
        - ``decisions``: per-tool decisions when ``decision_type`` is ``"batch"``
        - ``rejection_reasons``: optional per-tool rejection reasons
        - ``ask_user_answers``: optional answers when resuming an ``ask_user`` interrupt

        The BYO graph's interrupt handler is responsible for reading and
        acting on these fields.
        """
        # Extract decision from message using core utility
        decision_type = extract_decision_from_message(context.message)

        if not decision_type:
            # Security: Default to reject if decision cannot be determined
            logger.warning(
                f"Could not determine decision from message for task {context.task_id}, defaulting to reject"
            )
            decision_type = KAGENT_HITL_DECISION_TYPE_REJECT

        # Get thread_id from existing task metadata (critical for resume!)
        thread_id = None
        if context.current_task and context.current_task.metadata:
            thread_id = context.current_task.metadata.get("thread_id")

        if not thread_id:
            # Fallback to computing from context (same as initial)
            thread_id = getattr(context, "session_id", None) or context.context_id

        # Build the resume payload with all available HITL data.
        # The graph receives this as the return value of interrupt().
        resume_value: dict[str, Any] = {"decision_type": decision_type}

        if decision_type == KAGENT_HITL_DECISION_TYPE_BATCH:
            batch_decisions = extract_batch_decisions_from_message(context.message)
            if batch_decisions:
                resume_value["decisions"] = batch_decisions

        rejection_reasons = extract_rejection_reasons_from_message(context.message)
        if rejection_reasons:
            resume_value["rejection_reasons"] = rejection_reasons

        ask_user_answers = extract_ask_user_answers_from_message(context.message)
        if ask_user_answers:
            resume_value["ask_user_answers"] = ask_user_answers

        logger.info(
            "Resuming after interrupt - task_id=%s, thread_id=%s, decision=%s, has_batch=%s, has_reasons=%s, has_answers=%s",
            context.task_id,
            thread_id,
            decision_type,
            "decisions" in resume_value,
            "rejection_reasons" in resume_value,
            "ask_user_answers" in resume_value,
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
                    state=TaskState.working,
                    timestamp=datetime.now(UTC).isoformat(),
                ),
                context_id=context.context_id,
                final=False,
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
                        state=TaskState.failed,
                        timestamp=datetime.now(UTC).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=f"Resume failed: {str(e)}"))],
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
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

            # Send task submitted event for new tasks
            if not context.current_task:
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.submitted,
                            message=context.message,
                            timestamp=datetime.now(UTC).isoformat(),
                        ),
                        context_id=context.context_id,
                        final=False,
                    )
                )

            # Calculate and store thread_id for potential resume
            thread_id = getattr(context, "session_id", None) or context.context_id

            # Send working status
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.working,
                        timestamp=datetime.now(UTC).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=False,
                    metadata={
                        "app_name": self.app_name,
                        "session_id": getattr(context, "session_id", context.context_id),
                        "thread_id": thread_id,  # Store for resume!
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
                            state=TaskState.failed,
                            timestamp=datetime.now(UTC).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text="Execution timed out"))],
                            ),
                        ),
                        context_id=context.context_id,
                        final=True,
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
                            state=TaskState.failed,
                            timestamp=datetime.now(UTC).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=user_message))],
                                metadata={
                                    get_kagent_metadata_key("error_type"): error_meta["error_type"],
                                    get_kagent_metadata_key("error_detail"): error_meta["error_detail"],
                                },
                            ),
                        ),
                        context_id=context.context_id,
                        final=True,
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
