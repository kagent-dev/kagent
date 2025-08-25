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
from langchain_core.messages import (
    AIMessage,
    HumanMessage,
    ToolMessage,
)
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel

from langgraph.graph.state import CompiledStateGraph

logger = logging.getLogger(__name__)


class LangGraphAgentExecutorConfig(BaseModel):
    """Configuration for the LangGraphAgentExecutor."""

    # Maximum time to wait for graph execution (seconds)
    execution_timeout: float = 300.0

    # Whether to stream intermediate results
    enable_streaming: bool = True

    # User ID to use if not provided in request
    default_user_id: str = "admin@kagent.dev"


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
        input_data: dict[str, Any],
        config: RunnableConfig,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Stream LangGraph events and convert them to A2A events."""

        try:
            # Stream events from the graph
            async for event in graph.astream(
                input_data,
                config,
                stream_mode="updates",  # Stream the individual events
            ):
                logger.info(f"LangGraph event: {event}")

                # Convert LangGraph events to A2A events
                a2a_event = await self._convert_langgraph_event_to_a2a(event, context.task_id, context.context_id)
                if a2a_event:
                    logger.info(f"A2A event: {a2a_event}")
                    await event_queue.enqueue_event(a2a_event)

        except Exception as e:
            logger.error(f"Error during graph execution: {e}", exc_info=True)
            # Send failure event
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(UTC).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=f"Graph execution failed: {str(e)}"))],
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
            return

        # Final artifacts are already sent through individual event processing

        # Send completion status
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

    async def _convert_langgraph_event_to_a2a(
        self, langgraph_event: dict[str, Any], task_id: str, context_id: str
    ) -> TaskStatusUpdateEvent | None:
        """Convert a LangGraph event to A2A events."""

        # LangGraph events have node names as keys, with 'messages' as values
        # Example: {'agent': {'messages': [AIMessage(...)]}}
        for node_name, node_data in langgraph_event.items():
            if not isinstance(node_data, dict) or "messages" not in node_data:
                continue
            messages = node_data["messages"]
            if not isinstance(messages, list):
                continue

            # Process each message in the event
            for message in messages:
                if isinstance(message, AIMessage):
                    # Handle AI messages (assistant responses)
                    a2a_message = Message(message_id=str(uuid.uuid4()), role=Role.agent, parts=[])
                    if message.content and isinstance(message.content, str) and message.content.strip():
                        a2a_message.parts.append(Part(TextPart(text=message.content)))

                    # Handle tool calls in AI messages
                    if hasattr(message, "tool_calls") and message.tool_calls:
                        for tool_call in message.tool_calls:
                            tool_call_text = f"Calling tool: {tool_call['name']} with args: {tool_call['args']}"
                            a2a_message.parts.append(Part(TextPart(text=tool_call_text)))
                    return TaskStatusUpdateEvent(
                        task_id=task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(UTC).isoformat(),
                            message=a2a_message,
                        ),
                        context_id=context_id,
                        final=False,
                        metadata={
                            "app_name": self.app_name,
                            "session_id": context_id,
                        },
                    )

                elif isinstance(message, ToolMessage):
                    # Handle tool responses
                    if message.content and isinstance(message.content, str):
                        tool_response_text = f"Tool '{message.name}' returned: {message.content}"
                        return TaskStatusUpdateEvent(
                            task_id=task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(UTC).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=tool_response_text))],
                                ),
                            ),
                            context_id=context_id,
                            final=False,
                            metadata={
                                "app_name": self.app_name,
                                "session_id": context_id,
                            },
                        )

                elif isinstance(message, HumanMessage):
                    # Handle human messages (user input) - usually for context
                    if message.content and isinstance(message.content, str) and message.content.strip():
                        return TaskStatusUpdateEvent(
                            task_id=task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(UTC).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=f"User: {message.content}"))],
                                ),
                            ),
                            context_id=context_id,
                            final=False,
                            metadata={
                                "app_name": self.app_name,
                                "session_id": context_id,
                            },
                        )
        return None

    def _extract_content_from_output(self, output: dict[str, Any]) -> str:
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
                    "user_id": getattr(context, "user_id", self._config.default_user_id),
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
