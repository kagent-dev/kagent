import asyncio
import logging
import uuid
from datetime import datetime, timezone
from typing import Any, Union, override

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
from crewai.events import (
    AgentExecutionCompletedEvent,
    AgentExecutionStartedEvent,
    MethodExecutionFinishedEvent,
    MethodExecutionStartedEvent,
    TaskCompletedEvent,
    TaskStartedEvent,
    ToolUsageFinishedEvent,
    ToolUsageStartedEvent,
    crewai_event_bus,
)
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)

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
                    "session_id": getattr(context, "session_id", context.context_id),
                },
            )
        )

        loop = asyncio.get_running_loop()

        def _enqueue_event(event: Any):
            asyncio.run_coroutine_threadsafe(event_queue.enqueue_event(event), loop)

        # We do not care if it's a Crew or Flow kicking off because what matters is tasks, agents, and tools
        with crewai_event_bus.scoped_handlers():
            # NOTE: The task related listeners are just for simple logging purposes, the actual output is shown in the agent listener to avoid repeated content
            @crewai_event_bus.on(TaskStartedEvent)
            def on_task_started(source: Any, event: TaskStartedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=f"Task started: {event.task.name}"))],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            @crewai_event_bus.on(TaskCompletedEvent)
            def on_task_completed(source: Any, event: TaskCompletedEvent):
                if event.output:
                    _enqueue_event(
                        TaskStatusUpdateEvent(
                            task_id=context.task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(timezone.utc).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=f"Task completed: {event.task.name}\n"))],
                                ),
                            ),
                            context_id=context.context_id,
                            final=False,
                            metadata={"app_name": self.app_name, "session_id": context.context_id},
                        )
                    )

            @crewai_event_bus.on(AgentExecutionStartedEvent)
            def on_agent_execution_started(source: Any, event: AgentExecutionStartedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[
                                    Part(
                                        TextPart(
                                            text=f"Agent {event.agent_id} started working on task: {event.task_prompt}"
                                        )
                                    )
                                ],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            @crewai_event_bus.on(AgentExecutionCompletedEvent)
            def on_agent_execution_completed(source: Any, event: AgentExecutionCompletedEvent):
                if event.output:
                    _enqueue_event(
                        TaskStatusUpdateEvent(
                            task_id=context.task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(timezone.utc).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=str(event.output)))],
                                ),
                            ),
                            context_id=context.context_id,
                            final=False,
                            metadata={"app_name": self.app_name, "session_id": context.context_id},
                        )
                    )

            # Unlike langgraph tool usage message is not part of assistant message
            @crewai_event_bus.on(ToolUsageStartedEvent)
            def on_tool_usage_started(source: Any, event: ToolUsageStartedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[
                                    Part(
                                        DataPart(
                                            data={
                                                "id": event.tool_class,
                                                "name": event.tool_name,
                                                "args": event.tool_args,
                                            },
                                            metadata={
                                                get_kagent_metadata_key(
                                                    A2A_DATA_PART_METADATA_TYPE_KEY
                                                ): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
                                            },
                                        )
                                    )
                                ],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            @crewai_event_bus.on(ToolUsageFinishedEvent)
            def on_tool_usage_finished(source: Any, event: ToolUsageFinishedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[
                                    Part(
                                        DataPart(
                                            data={
                                                "id": event.tool_class,
                                                "name": event.tool_name,
                                                "response": event.output,
                                            },
                                            metadata={
                                                get_kagent_metadata_key(
                                                    A2A_DATA_PART_METADATA_TYPE_KEY
                                                ): A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
                                            },
                                        )
                                    )
                                ],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            @crewai_event_bus.on(MethodExecutionStartedEvent)
            def on_method_execution_started(source: Any, event: MethodExecutionStartedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[
                                    Part(
                                        TextPart(
                                            text=f"Method {event.method_name} from flow {event.flow_name} started execution."
                                        )
                                    )
                                ],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            @crewai_event_bus.on(MethodExecutionFinishedEvent)
            def on_method_execution_finished(source: Any, event: MethodExecutionFinishedEvent):
                _enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[
                                    Part(
                                        TextPart(
                                            text=f"Method {event.method_name} from flow {event.flow_name} finished execution."
                                        )
                                    )
                                ],
                            ),
                        ),
                        context_id=context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": context.context_id},
                    )
                )

            try:
                inputs = None
                if context.message and context.message.parts:
                    for part in context.message.parts:
                        if isinstance(part, DataPart):
                            # Using structured inputs for replacing fields in Task
                            inputs = part.root.data
                            break
                if inputs is None:
                    # This gets all the TextParts from the Message
                    user_input = context.get_user_input()
                    inputs = {"input": user_input} if user_input else {}

                # Handle Flow vs Crew differently
                if isinstance(self._crew, Flow):
                    # For Flows, create a new instance for each execution to avoid state issues
                    flow_class = type(self._crew)
                    flow_instance = flow_class()
                    result = await flow_instance.kickoff_async(inputs=inputs)
                else:
                    # For Crews, use the existing instance
                    result = await self._crew.kickoff_async(inputs=inputs)

                # Send final result
                await event_queue.enqueue_event(
                    TaskArtifactUpdateEvent(
                        task_id=context.task_id,
                        last_chunk=True,
                        context_id=context.context_id,
                        artifact=Artifact(
                            artifact_id=str(uuid.uuid4()),
                            # result.raw is the text output of the final result
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
