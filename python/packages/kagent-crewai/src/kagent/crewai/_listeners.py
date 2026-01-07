import asyncio
import uuid
from datetime import datetime, timezone
from typing import Any

from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    DataPart,
    Message,
    Part,
    Role,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)

from crewai.events import (
    AgentExecutionCompletedEvent,
    AgentExecutionStartedEvent,
    BaseEventListener,
    MethodExecutionFinishedEvent,
    MethodExecutionStartedEvent,
    TaskCompletedEvent,
    TaskStartedEvent,
    ToolUsageFinishedEvent,
    ToolUsageStartedEvent,
)
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)


class A2ACrewAIListener(BaseEventListener):
    def __init__(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        app_name: str,
    ):
        super().__init__()
        self.context = context
        self.event_queue = event_queue
        self.app_name = app_name
        self.loop = asyncio.get_running_loop()

    def _enqueue_event(self, event: Any):
        asyncio.run_coroutine_threadsafe(self.event_queue.enqueue_event(event), self.loop)

    def setup_listeners(self, crewai_event_bus):
        @crewai_event_bus.on(TaskStartedEvent)
        def on_task_started(source: Any, event: TaskStartedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
                    status=TaskStatus(
                        state=TaskState.working,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[Part(TextPart(text=f"Task started: {event.task.name}"))],
                        ),
                    ),
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )

        @crewai_event_bus.on(TaskCompletedEvent)
        def on_task_completed(source: Any, event: TaskCompletedEvent):
            if event.output:
                self._enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=self.context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=f"Task completed: {event.task.name}\n"))],
                            ),
                        ),
                        context_id=self.context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                    )
                )

        @crewai_event_bus.on(AgentExecutionStartedEvent)
        def on_agent_execution_started(source: Any, event: AgentExecutionStartedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
                    status=TaskStatus(
                        state=TaskState.working,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=Message(
                            message_id=str(uuid.uuid4()),
                            role=Role.agent,
                            parts=[
                                Part(
                                    TextPart(
                                        text=f"Agent {event.agent.id} started working on task: {event.task_prompt}"
                                    )
                                )
                            ],
                        ),
                    ),
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )

        @crewai_event_bus.on(AgentExecutionCompletedEvent)
        def on_agent_execution_completed(source: Any, event: AgentExecutionCompletedEvent):
            if event.output:
                self._enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=self.context.task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=str(event.output)))],
                            ),
                        ),
                        context_id=self.context.context_id,
                        final=False,
                        metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                    )
                )

        @crewai_event_bus.on(ToolUsageStartedEvent)
        def on_tool_usage_started(source: Any, event: ToolUsageStartedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
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
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )

        @crewai_event_bus.on(ToolUsageFinishedEvent)
        def on_tool_usage_finished(source: Any, event: ToolUsageFinishedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
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
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )

        @crewai_event_bus.on(MethodExecutionStartedEvent)
        def on_method_execution_started(source: Any, event: MethodExecutionStartedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
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
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )

        @crewai_event_bus.on(MethodExecutionFinishedEvent)
        def on_method_execution_finished(source: Any, event: MethodExecutionFinishedEvent):
            self._enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=self.context.task_id,
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
                    context_id=self.context.context_id,
                    final=False,
                    metadata={"app_name": self.app_name, "session_id": self.context.context_id},
                )
            )
