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
            }
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

        # Stream events from the graph
        async for event in graph.astream(
            input_data,
            config,
            stream_mode="updates",  # Stream the individual events
        ):
            # Convert LangGraph events to A2A events
            a2a_events = await _convert_langgraph_event_to_a2a(
                event, context.task_id, context.context_id, self.app_name
            )
            for a2a_event in a2a_events:
                task_result_aggregator.process_event(a2a_event)
                await event_queue.enqueue_event(a2a_event)

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

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not implemented")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        """Execute the LangGraph workflow and publish updates to the event queue."""
        if not context.message:
            raise ValueError("A2A request must have a message")

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
