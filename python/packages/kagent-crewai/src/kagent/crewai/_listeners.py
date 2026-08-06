import asyncio
import json
import uuid
from typing import Any

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
)
from google.protobuf.json_format import ParseDict
from google.protobuf.struct_pb2 import Value
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
    now_timestamp,
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


def _agent_tool_name(agent_name: str) -> str:
    """Encode an agent name so the UI renders it via AgentCallDisplay (__NS__)."""
    safe = agent_name.replace(" ", "_")
    if "/" in safe:
        return safe.replace("/", "__NS__")
    return f"{safe}__NS__agent"


def _agent_display_name(agent: Any) -> str:
    return getattr(agent, "role", None) or getattr(agent, "name", None) or str(getattr(agent, "id", "agent"))


def _agent_call_id(agent: Any, task: Any) -> str:
    agent_id = str(getattr(agent, "id", None) or _agent_display_name(agent))
    task_id = getattr(task, "id", None) if task is not None else None
    return f"{agent_id}:{task_id}" if task_id else agent_id


def _as_args(raw: Any) -> dict:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str):
        try:
            parsed = json.loads(raw)
            return parsed if isinstance(parsed, dict) else {"raw": raw}
        except (json.JSONDecodeError, TypeError):
            return {"raw": raw}
    if raw is None:
        return {}
    return {"raw": str(raw)}


class A2ACrewAIListener(BaseEventListener):
    def __init__(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        app_name: str,
    ):
        # Handlers close over self; fields must exist before super() registers them.
        self.context = context
        self.event_queue = event_queue
        self.app_name = app_name
        self.loop = asyncio.get_running_loop()
        # Stack of in-flight tool call IDs keyed by a stable tool invocation fingerprint.
        self._tool_call_ids: dict[tuple[str, str, str, str], list[str]] = {}
        super().__init__()

    def _enqueue_event(self, event: Any):
        asyncio.run_coroutine_threadsafe(self.event_queue.enqueue_event(event), self.loop)

    def _base_metadata(self) -> dict[str, str]:
        return {
            get_kagent_metadata_key("app_name"): self.app_name,
            get_kagent_metadata_key("session_id"): self.context.context_id or "",
        }

    def _enqueue_parts(self, parts: list[Part], *, event_type: str | None = None):
        metadata = self._base_metadata()
        if event_type:
            metadata[get_kagent_metadata_key("event_type")] = event_type
        self._enqueue_event(
            TaskArtifactUpdateEvent(
                task_id=self.context.task_id,
                context_id=self.context.context_id,
                last_chunk=True,
                artifact=Artifact(artifact_id=str(uuid.uuid4()), parts=parts, metadata=metadata),
                metadata=metadata,
            )
        )

    def _enqueue_status(self, text: str):
        """Emit WORKING status for progress tracking (not chat transcript)."""
        metadata = self._base_metadata()
        self._enqueue_event(
            TaskStatusUpdateEvent(
                task_id=self.context.task_id,
                context_id=self.context.context_id,
                status=TaskStatus(
                    state=TaskState.TASK_STATE_WORKING,
                    message=Message(
                        message_id=str(uuid.uuid4()),
                        role=Role.ROLE_AGENT,
                        parts=[Part(text=text)],
                    ),
                    timestamp=now_timestamp(),
                ),
                metadata=metadata,
            )
        )

    def _enqueue_function_part(self, data: dict, part_type: str, *, event_type: str):
        self._enqueue_parts(
            [
                Part(
                    data=ParseDict(data, Value()),
                    metadata={get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): part_type},
                )
            ],
            event_type=event_type,
        )

    def _tool_key(self, event: ToolUsageStartedEvent | ToolUsageFinishedEvent) -> tuple[str, str, str, str]:
        return (
            event.tool_name or "",
            event.agent_id or "",
            event.task_id or "",
            json.dumps(_as_args(event.tool_args), sort_keys=True, default=str),
        )

    def _begin_tool_call(self, event: ToolUsageStartedEvent) -> str:
        call_id = str(uuid.uuid4())
        self._tool_call_ids.setdefault(self._tool_key(event), []).append(call_id)
        return call_id

    def _end_tool_call(self, event: ToolUsageFinishedEvent) -> str:
        key = self._tool_key(event)
        stack = self._tool_call_ids.get(key)
        if stack:
            call_id = stack.pop(0)
            if not stack:
                del self._tool_call_ids[key]
            return call_id
        return str(uuid.uuid4())

    def setup_listeners(self, crewai_event_bus):
        @crewai_event_bus.on(TaskStartedEvent)
        def on_task_started(source: Any, event: TaskStartedEvent):
            task_name = getattr(event.task, "name", None) or "task"
            self._enqueue_status(f"Task started: {task_name}")

        @crewai_event_bus.on(TaskCompletedEvent)
        def on_task_completed(source: Any, event: TaskCompletedEvent):
            task_name = getattr(event.task, "name", None) or "task"
            self._enqueue_status(f"Task completed: {task_name}")

        @crewai_event_bus.on(MethodExecutionStartedEvent)
        def on_method_execution_started(source: Any, event: MethodExecutionStartedEvent):
            self._enqueue_status(f"Flow {event.flow_name}: {event.method_name} started")

        @crewai_event_bus.on(MethodExecutionFinishedEvent)
        def on_method_execution_finished(source: Any, event: MethodExecutionFinishedEvent):
            self._enqueue_status(f"Flow {event.flow_name}: {event.method_name} finished")

        @crewai_event_bus.on(AgentExecutionStartedEvent)
        def on_agent_execution_started(source: Any, event: AgentExecutionStartedEvent):
            agent_name = _agent_display_name(event.agent)
            self._enqueue_function_part(
                {
                    "id": _agent_call_id(event.agent, event.task),
                    "name": _agent_tool_name(agent_name),
                    "args": {"task": event.task_prompt or ""},
                },
                A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
                event_type="agent_execution",
            )

        @crewai_event_bus.on(AgentExecutionCompletedEvent)
        def on_agent_execution_completed(source: Any, event: AgentExecutionCompletedEvent):
            agent_name = _agent_display_name(event.agent)
            self._enqueue_function_part(
                {
                    "id": _agent_call_id(event.agent, event.task),
                    "name": _agent_tool_name(agent_name),
                    "response": {"result": event.output if event.output is not None else ""},
                },
                A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
                event_type="agent_execution",
            )

        @crewai_event_bus.on(ToolUsageStartedEvent)
        def on_tool_usage_started(source: Any, event: ToolUsageStartedEvent):
            call_id = self._begin_tool_call(event)
            self._enqueue_function_part(
                {
                    "id": call_id,
                    "name": event.tool_name,
                    "args": _as_args(event.tool_args),
                },
                A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
                event_type="tool_call",
            )

        @crewai_event_bus.on(ToolUsageFinishedEvent)
        def on_tool_usage_finished(source: Any, event: ToolUsageFinishedEvent):
            call_id = self._end_tool_call(event)
            self._enqueue_function_part(
                {
                    "id": call_id,
                    "name": event.tool_name,
                    "response": {"result": event.output},
                },
                A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
                event_type="tool_output",
            )
