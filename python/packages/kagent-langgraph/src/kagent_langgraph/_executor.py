"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import asyncio
import logging
import uuid
from datetime import datetime, timezone
from typing import Any, AsyncIterator, Awaitable, Callable, Dict, Optional, Union

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    Message,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from langchain_core.messages import BaseMessage, HumanMessage
from langchain_core.runnables import RunnableConfig
from langgraph.graph.state import CompiledStateGraph, StateGraph
from pydantic import BaseModel
from typing_extensions import override

logger = logging.getLogger(__name__)


class LangGraphAgentExecutorConfig(BaseModel):
    """Configuration for the LangGraphAgentExecutor."""

    # Maximum time to wait for graph execution (seconds)
    execution_timeout: float = 300.0

    # Whether to stream intermediate results
    enable_streaming: bool = True

    # User ID to use if not provided in request
    default_user_id: str = "admin@kagent.dev"


class TaskResultAggregator:
    """Aggregates task results from LangGraph events."""

    def __init__(self):
        self.task_state = TaskState.working
        self.task_status_message: Optional[Message] = None
        self.accumulated_content: list[str] = []

    def process_event(self, event: Union[TaskArtifactUpdateEvent, TaskStatusUpdateEvent]):
        """Process an A2A event and update internal state."""
        if isinstance(event, TaskArtifactUpdateEvent):
            # Extract text content from artifact parts
            if event.artifact and event.artifact.parts:
                for part in event.artifact.parts:
                    if hasattr(part, "text") and part.text:
                        self.accumulated_content.append(part.text)

        elif isinstance(event, TaskStatusUpdateEvent):
            self.task_state = event.status.state
            if event.status.message:
                self.task_status_message = event.status.message

    def get_final_message(self) -> Optional[Message]:
        """Get the final aggregated message."""
        if self.accumulated_content:
            content = "".join(self.accumulated_content)
            return Message(message_id=str(uuid.uuid4()), role=Role.agent, parts=[TextPart(text=content)])
        return self.task_status_message


class LangGraphAgentExecutor(AgentExecutor):
    """An AgentExecutor that runs LangGraph workflows against A2A requests.

    This executor integrates LangGraph with the A2A protocol, handling session
    management, event streaming, and result aggregation.
    """

    def __init__(
        self,
        *,
        graph: Union[CompiledStateGraph, Callable[..., Union[CompiledStateGraph, Awaitable[CompiledStateGraph]]]],
        app_name: str,
        config: Optional[LangGraphAgentExecutorConfig] = None,
    ):
        """Initialize the executor.

        Args:
            graph: Compiled LangGraph or factory function that returns one
            app_name: Application name for session management
            config: Optional executor configuration
        """
        super().__init__()
        self._graph = graph
        self.app_name = app_name
        self._config = config or LangGraphAgentExecutorConfig()

    async def _resolve_graph(self) -> CompiledStateGraph:
        """Resolve the graph, handling cases where it's a callable."""
        if callable(self._graph):
            result = self._graph()
            if asyncio.iscoroutine(result):
                resolved_graph = await result
            else:
                resolved_graph = result

            if not hasattr(resolved_graph, "ainvoke"):
                raise TypeError(f"Graph factory must return a CompiledGraph, got {type(resolved_graph)}")

            return resolved_graph

        return self._graph

    def _convert_a2a_message_to_langchain(self, message: Message) -> BaseMessage:
        """Convert A2A message to LangChain message format."""
        # Extract text content from message parts
        text_content = []
        for part in message.parts:
            if hasattr(part, "text") and part.text:
                text_content.append(part.text)

        content = " ".join(text_content) if text_content else ""

        # Convert to appropriate message type based on role
        if message.role == Role.user:
            return HumanMessage(content=content)
        else:
            # For now, treat all non-user messages as human messages
            # This could be extended to support other message types
            return HumanMessage(content=content)

    def _create_graph_config(self, context: RequestContext) -> RunnableConfig:
        """Create LangGraph config from A2A request context."""
        # Extract session information
        session_id = getattr(context, "session_id", None) or context.context_id
        user_id = getattr(context, "user_id", self._config.default_user_id)

        return {
            "configurable": {
                "thread_id": session_id,
                "user_id": user_id,
                "app_name": self.app_name,
            }
        }

    async def _stream_graph_events(
        self,
        graph: CompiledStateGraph,
        input_data: Dict[str, Any],
        config: RunnableConfig,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Stream LangGraph events and convert them to A2A events."""
        task_result_aggregator = TaskResultAggregator()

        try:
            # Stream events from the graph
            async for event in graph.astream_events(input_data, config, version="v2"):
                # Convert LangGraph events to A2A events
                a2a_events = await self._convert_langgraph_event_to_a2a(event, context.task_id, context.context_id)

                for a2a_event in a2a_events:
                    task_result_aggregator.process_event(a2a_event)
                    await event_queue.enqueue_event(a2a_event)

        except Exception as e:
            logger.error(f"Error during graph execution: {e}", exc_info=True)
            # Send failure event
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[TextPart(text=f"Graph execution failed: {str(e)}")],
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
            return

        # Send final completion event
        final_message = task_result_aggregator.get_final_message()

        if final_message and final_message.parts:
            # Send final artifact
            await event_queue.enqueue_event(
                TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    last_chunk=True,
                    context_id=context.context_id,
                    artifact=Artifact(
                        artifact_id=str(uuid.uuid4()),
                        parts=final_message.parts,
                    ),
                )
            )

        # Send completion status
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.completed,
                    timestamp=datetime.now(timezone.utc).isoformat(),
                ),
                context_id=context.context_id,
                final=True,
            )
        )

    async def _convert_langgraph_event_to_a2a(
        self, langgraph_event: Dict[str, Any], task_id: str, context_id: str
    ) -> list[Union[TaskArtifactUpdateEvent, TaskStatusUpdateEvent]]:
        """Convert a LangGraph event to A2A events."""
        events = []

        event_type = langgraph_event.get("event")
        event_data = langgraph_event.get("data", {})

        # Handle different LangGraph event types
        if event_type == "on_chat_model_stream" and "chunk" in event_data:
            # Streaming token from language model
            chunk = event_data["chunk"]
            if hasattr(chunk, "content") and chunk.content:
                events.append(
                    TaskArtifactUpdateEvent(
                        task_id=task_id,
                        last_chunk=False,
                        context_id=context_id,
                        artifact=Artifact(
                            artifact_id=str(uuid.uuid4()),
                            parts=[TextPart(text=chunk.content)],
                        ),
                    )
                )

        elif event_type == "on_chain_end":
            # Chain/node completion - could contain final output
            output = event_data.get("output")
            if output and isinstance(output, dict):
                # Try to extract meaningful content
                content = self._extract_content_from_output(output)
                if content:
                    events.append(
                        TaskArtifactUpdateEvent(
                            task_id=task_id,
                            last_chunk=False,
                            context_id=context_id,
                            artifact=Artifact(
                                artifact_id=str(uuid.uuid4()),
                                parts=[TextPart(text=content)],
                            ),
                        )
                    )

        # Add more event type handling as needed

        return events

    def _extract_content_from_output(self, output: Dict[str, Any]) -> str:
        """Extract meaningful text content from LangGraph output."""
        # Handle common output formats
        if "messages" in output:
            messages = output["messages"]
            if messages and isinstance(messages, list):
                last_message = messages[-1]
                if hasattr(last_message, "content"):
                    return str(last_message.content)

        # Handle direct text output
        if isinstance(output, str):
            return output

        # Handle other structured outputs
        for key in ["response", "answer", "result", "output"]:
            if key in output:
                value = output[key]
                if isinstance(value, str):
                    return value
                elif hasattr(value, "content"):
                    return str(value.content)

        return ""

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.cancelled,
                    timestamp=datetime.now(timezone.utc).isoformat(),
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

        # Send task submitted event for new tasks
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

        # Send working status
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.working,
                    timestamp=datetime.now(timezone.utc).isoformat(),
                ),
                context_id=context.context_id,
                final=False,
                metadata={
                    "app_name": self.app_name,
                    "session_id": getattr(context, "session_id", context.context_id),
                    "user_id": getattr(context, "user_id", self._config.default_user_id),
                },
            )
        )

        try:
            # Resolve the graph
            graph = await self._resolve_graph()

            # Convert A2A message to LangChain format
            langchain_message = self._convert_a2a_message_to_langchain(context.message)

            # Prepare input for the graph
            # This assumes the graph expects a "messages" key - adjust as needed
            input_data = {"messages": [langchain_message]}

            # Create graph config
            config = self._create_graph_config(context)

            # Stream graph execution
            await asyncio.wait_for(
                self._stream_graph_events(graph, input_data, config, context, event_queue),
                timeout=self._config.execution_timeout,
            )

        except asyncio.TimeoutError:
            logger.error(f"Graph execution timed out after {self._config.execution_timeout} seconds")
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()), role=Role.agent, parts=[TextPart(text="Execution timed out")]
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
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(message_id=str(uuid.uuid4()), role=Role.agent, parts=[TextPart(text=str(e))]),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
