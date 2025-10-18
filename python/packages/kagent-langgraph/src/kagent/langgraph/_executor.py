"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import asyncio
import logging
import uuid
from datetime import UTC, datetime
from typing import Any, override

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

from kagent.core.a2a import TaskResultAggregator
from langgraph.graph.state import CompiledStateGraph

from ._converters import _convert_langgraph_event_to_a2a

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

        # Stream events from the graph
        async for event in graph.astream(
            input_data,
            config,
            stream_mode="updates",  # Stream the individual events
        ):
            # Store final state
            final_state = event

            # Convert LangGraph events to A2A events
            a2a_events = await _convert_langgraph_event_to_a2a(
                event, context.task_id, context.context_id, self.app_name
            )
            for a2a_event in a2a_events:
                task_result_aggregator.process_event(a2a_event)
                await event_queue.enqueue_event(a2a_event)

        # Check for interrupts after streaming completes
        interrupt_detected = False
        if final_state and "__interrupt__" in final_state:
            interrupt_detected = True
            interrupt_data = final_state["__interrupt__"]
            await self._handle_interrupt(
                interrupt_data=interrupt_data,
                task_id=context.task_id,
                context_id=context.context_id,
                event_queue=event_queue,
                task_store=context.task_store,
            )
            # Don't return early - let the task be saved by consumer
            # The input_required event with final=False allows task to be resumed

        # Final artifacts are already sent through individual event processing

        # publish the task result event - this is final
        # Skip completion events if interrupted (already sent input_required)
        if not interrupt_detected and (
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
        elif not interrupt_detected:
            # Only send final event if not interrupted
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
        # If interrupted, don't send any final event - input_required was already sent

    async def _handle_interrupt(
        self,
        interrupt_data: list[Any],
        task_id: str,
        context_id: str,
        event_queue: EventQueue,
        task_store: "TaskStore",
    ) -> None:
        """Handle interrupt from LangGraph and convert to A2A input_required event."""
        from kagent.core.a2a import get_kagent_metadata_key

        # Extract interrupt details
        # Format: [{"value": {"action_requests": [...], "review_configs": [...]}}]
        if not interrupt_data or len(interrupt_data) == 0:
            logger.warning("Empty interrupt data received")
            return

        interrupt_value = interrupt_data[0].value if hasattr(interrupt_data[0], "value") else interrupt_data[0]
        action_requests = interrupt_value.get("action_requests", [])
        review_configs = interrupt_value.get("review_configs", [])

        # Build approval message for user
        parts = []

        # Add header
        parts.append(Part(TextPart(text="**Approval Required**\n\n")))
        parts.append(Part(TextPart(text="The following actions require your approval:\n\n")))

        # List each action
        for action in action_requests:
            tool_name = action.get("name", "unknown")
            tool_args = action.get("args", {})

            parts.append(Part(TextPart(text=f"**Tool**: `{tool_name}`\n")))
            parts.append(Part(TextPart(text="**Arguments**:\n")))
            for key, value in tool_args.items():
                parts.append(Part(TextPart(text=f"  • {key}: `{value}`\n")))
            parts.append(Part(TextPart(text="\n")))

        # Add approval metadata as DataPart for slackbot to parse
        parts.append(Part(DataPart(
            data={
                "interrupt_type": "tool_approval",
                "action_requests": action_requests,
                "review_configs": review_configs,
            },
            metadata={
                get_kagent_metadata_key("type"): "interrupt_data"
            }
        )))

        # Send input_required event
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
                final=False,  # Not final - waiting for user input
                metadata={
                    "interrupt_type": "tool_approval",
                    "app_name": self.app_name,
                },
            )
        )

        logger.info(
            f"Interrupt detected, sent input_required event for task {task_id} with {len(action_requests)} actions"
        )

        # Wait for the event consumer to persist the task (event-based sync)
        # This prevents race condition where approval arrives before task is saved
        try:
            await task_store.wait_for_save(task_id, timeout=5.0)
        except asyncio.TimeoutError:
            logger.warning(f"Task save event timeout, proceeding anyway")

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not implemented")

    def _is_resume_command(self, context: RequestContext) -> bool:
        """Check if message is a resume command for an interrupted task."""
        # Must have an existing task in input_required state to resume
        if not context.current_task or context.current_task.status.state != TaskState.input_required:
            return False

        message = context.message
        if not message or not message.parts:
            return False

        for part in message.parts:
            # Part is a RootModel union - need to access .root to get TextPart/FilePart/DataPart
            if not hasattr(part, "root"):
                continue

            inner = part.root

            # Priority 1: Check for structured decision in DataPart (most reliable)
            if isinstance(inner, DataPart):
                data = inner.data
                if data.get("decision_type") == "tool_approval":
                    return True

            # Priority 2: Check for keywords in TextPart (fallback)
            elif isinstance(inner, TextPart):
                text = inner.text
                if text and isinstance(text, str):
                    text_lower = text.lower()
                    if any(keyword in text_lower for keyword in ["approved", "denied", "proceed", "cancel"]):
                        return True

        return False

    async def _handle_resume(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Resume graph execution after interrupt with user decision."""
        from langgraph.types import Command

        # Determine decision from message
        message_text = context.get_user_input().lower()

        if "approved" in message_text or "proceed" in message_text:
            decision_type = "approve"
        elif "denied" in message_text or "cancel" in message_text:
            decision_type = "reject"
        else:
            decision_type = "approve"  # Default to approve for safety

        # Get thread_id from existing task metadata (critical for resume!)
        thread_id = None
        if context.current_task and context.current_task.metadata:
            thread_id = context.current_task.metadata.get("thread_id")

        if not thread_id:
            # Fallback to computing from context (same as initial)
            thread_id = getattr(context, "session_id", None) or context.context_id

        logger.info(
            f"Resuming after interrupt - task_id={context.task_id}, "
            f"thread_id={thread_id}, decision={decision_type}"
        )

        # Create resume input
        resume_input = Command(resume={"decisions": [{"type": decision_type}]})

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
                    event_queue
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
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(UTC).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=str(e)))],
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
