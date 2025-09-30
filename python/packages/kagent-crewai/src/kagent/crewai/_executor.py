import logging
import uuid
from datetime import datetime, timezone
from typing import Union, override

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
from pydantic import BaseModel

from crewai import Crew, Flow

from ._listeners import A2ACrewAIListener

logger = logging.getLogger(__name__)


class CrewAIAgentExecutorConfig(BaseModel):
    execution_timeout: float = 300.0


class CrewAIAgentExecutor(AgentExecutor):
    def __init__(
        self,
        *,
        crew: Union[Crew, Flow],
        app_name: str,
        config: CrewAIAgentExecutorConfig | None = None,
    ):
        super().__init__()
        self._crew = crew
        self.app_name = app_name
        self._config = config or CrewAIAgentExecutorConfig()

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        raise NotImplementedError("Cancellation is not implemented")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        if not context.message:
            raise ValueError("A2A request must have a message")

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
                    "session_id": context.context_id,
                },
            )
        )

        # This listener will capture and convert CrewAI events and enqueue them to A2A event queue
        A2ACrewAIListener(context, event_queue, self.app_name)

        try:
            inputs = None
            if context.message and context.message.parts:
                for part in context.message.parts:
                    if isinstance(part, DataPart):
                        inputs = part.root.data
                        break
            if inputs is None:
                user_input = context.get_user_input()
                inputs = {"input": user_input} if user_input else {}

            if isinstance(self._crew, Flow):
                flow_class = type(self._crew)
                flow_instance = flow_class()
                result = await flow_instance.kickoff_async(inputs=inputs)
            else:
                result = await self._crew.kickoff_async(inputs=inputs)

            await event_queue.enqueue_event(
                TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    last_chunk=True,
                    context_id=context.context_id,
                    artifact=Artifact(
                        artifact_id=str(uuid.uuid4()),
                        parts=[Part(TextPart(text=str(result.raw)))],
                    ),
                )
            )
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

        except Exception as e:
            logger.error(f"Error during CrewAI execution: {e}", exc_info=True)
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.failed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
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
