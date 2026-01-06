"""OpenAI Agent Executor for A2A Protocol.

This module implements an agent executor that runs OpenAI Agents SDK agents
within the A2A (Agent-to-Agent) protocol, converting streaming events to A2A events.
"""

from __future__ import annotations

import asyncio
import logging
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import override

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
from agents.agent import Agent
from agents.run import Runner
from pydantic import BaseModel

from kagent.core.a2a import TaskResultAggregator, get_kagent_metadata_key

from ._event_converter import convert_openai_event_to_a2a_events
from ._session_service import KAgentSession

logger = logging.getLogger(__name__)


class OpenAIAgentExecutorConfig(BaseModel):
    """Configuration for the OpenAIAgentExecutor."""

    # Maximum time to wait for agent execution (seconds)
    execution_timeout: float = 300.0


@dataclass
class SessionContext:
    """Context information for a KAgent session."""

    session_id: str


class OpenAIAgentExecutor(AgentExecutor):
    """An AgentExecutor that runs OpenAI Agents SDK agents against A2A requests.

    This executor integrates OpenAI Agents SDK with the A2A protocol, handling
    session management, event streaming, and result aggregation.
    """

    def __init__(
        self,
        *,
        agent: Agent | Callable[[], Agent],
        app_name: str,
        session_factory: Callable[[str, str], KAgentSession] | None = None,
        config: OpenAIAgentExecutorConfig | None = None,
    ):
        """Initialize the executor.

        Args:
            agent: OpenAI Agent instance or factory function that returns an agent
            app_name: Application name for session management
            session_factory: Optional factory for creating KAgentSession instances
            config: Optional executor configuration
        """
        super().__init__()
        self._agent = agent
        self.app_name = app_name
        self._session_factory = session_factory
        self._config = config or OpenAIAgentExecutorConfig()

    def _resolve_agent(self) -> Agent:
        """Resolve the agent, handling both instances and factory functions."""
        if callable(self._agent):
            # Call the factory to get the agent
            return self._agent()
        return self._agent

    async def _stream_agent_events(
        self,
        agent: Agent,
        user_input: str,
        session: KAgentSession | None,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        """Stream agent execution events and convert them to A2A events."""
        task_result_aggregator = TaskResultAggregator()
        session_context = SessionContext(session_id=session.session_id)

        try:
            # Use run_streamed for streaming support
            result = Runner.run_streamed(
                agent,
                user_input,
                session=session,
                context=session_context,
            )

            # Process streaming events
            async for event in result.stream_events():
                # Convert OpenAI event to A2A events
                a2a_events = convert_openai_event_to_a2a_events(
                    event,
                    context.task_id,
                    context.context_id,
                    self.app_name,
                )

                for a2a_event in a2a_events:
                    task_result_aggregator.process_event(a2a_event)
                    await event_queue.enqueue_event(a2a_event)

            # Handle final output
            if hasattr(result, "final_output") and result.final_output:
                final_message = Message(
                    message_id=str(uuid.uuid4()),
                    role=Role.agent,
                    parts=[Part(TextPart(text=str(result.final_output)))],
                )

                # Publish final artifact
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

                # Publish completion status
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
                # No output - publish based on aggregator state
                if (
                    task_result_aggregator.task_state == TaskState.working
                    and task_result_aggregator.task_status_message is not None
                    and task_result_aggregator.task_status_message.parts
                ):
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

        except Exception as e:
            logger.error(f"Error during agent execution: {e}", exc_info=True)
            raise

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
        """Execute the OpenAI agent and publish updates to the event queue."""
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

        # Extract session ID from context
        session_id = getattr(context, "session_id", None) or context.context_id
        user_id = getattr(context, "user_id", "admin@kagent.dev")

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
                    get_kagent_metadata_key("app_name"): self.app_name,
                    get_kagent_metadata_key("session_id"): session_id,
                    get_kagent_metadata_key("user_id"): user_id,
                },
            )
        )

        try:
            # Resolve the agent
            agent = self._resolve_agent()

            # Get user input from A2A message
            user_input = context.get_user_input()

            # Create session if factory is provided
            session = None
            if self._session_factory:
                session = self._session_factory(session_id, user_id)

            # Stream agent execution
            await asyncio.wait_for(
                self._stream_agent_events(
                    agent,
                    user_input,
                    session,
                    context,
                    event_queue,
                ),
                timeout=self._config.execution_timeout,
            )

        except TimeoutError:
            logger.error(f"Agent execution timed out after {self._config.execution_timeout} seconds")
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
            logger.error(f"Error during OpenAI agent execution: {e}", exc_info=True)

            error_message = str(e)

            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(UTC).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=f"Execution failed: {error_message}"))],
                            metadata={
                                get_kagent_metadata_key("error_type"): type(e).__name__,
                                get_kagent_metadata_key("error_detail"): error_message,
                            },
                        ),
                    ),
                    context_id=context.context_id,
                    final=True,
                    metadata={
                        get_kagent_metadata_key("error_type"): type(e).__name__,
                        get_kagent_metadata_key("error_detail"): error_message,
                    },
                )
            )
