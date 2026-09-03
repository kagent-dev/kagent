from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.types import Message, Part, Role, SendMessageRequest, TaskArtifactUpdateEvent
from google.protobuf.json_format import ParseDict
from google.protobuf.struct_pb2 import Value

from kagent.crewai._executor import CrewAIAgentExecutor, _convert_a2a_request_to_span_attributes


def _request_context(*parts: Part) -> RequestContext:
    message = Message(role=Role.ROLE_USER, message_id="msg-1", parts=list(parts))
    return RequestContext(
        call_context=MagicMock(),
        request=SendMessageRequest(message=message),
        task_id="task-1",
        context_id="ctx-1",
    )


def _make_crew() -> MagicMock:
    crew = MagicMock()
    # Skip the long-term memory branch, which needs a live base URL.
    crew.memory = False
    crew.kickoff_async = AsyncMock(return_value=MagicMock(raw="done"))
    return crew


class _RecordingEventQueue:
    def __init__(self):
        self.events = []

    async def enqueue_event(self, event):
        self.events.append(event)


async def _run(crew: MagicMock, context: RequestContext) -> list:
    executor = CrewAIAgentExecutor(
        crew=crew,
        app_name="test",
    )
    event_queue = _RecordingEventQueue()
    await executor.execute(context, event_queue)
    return event_queue.events


def _assert_content_artifact_closes_stream(events: list) -> None:
    artifacts = [event for event in events if isinstance(event, TaskArtifactUpdateEvent)]
    assert artifacts
    assert all(artifact.artifact.parts for artifact in artifacts)
    assert artifacts[-1].last_chunk is True


@pytest.mark.asyncio
async def test_execute_passes_datapart_data_as_inputs():
    crew = _make_crew()
    context = _request_context(Part(data=ParseDict({"topic": "ai"}, Value())))

    events = await _run(crew, context)

    crew.kickoff_async.assert_awaited_once_with(inputs={"topic": "ai"})
    _assert_content_artifact_closes_stream(events)


@pytest.mark.asyncio
async def test_execute_falls_back_to_text_input_without_datapart():
    crew = _make_crew()
    context = _request_context(Part(text="hello"))

    events = await _run(crew, context)

    crew.kickoff_async.assert_awaited_once_with(inputs={"input": "hello"})
    _assert_content_artifact_closes_stream(events)


def test_span_attributes_skip_decode_when_allowlist_empty(monkeypatch):
    from kagent.core.tracing._context_attributes import TRACE_CONTEXT_KEYS_ENV_VAR, _allowed_context_mappings

    monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
    _allowed_context_mappings.cache_clear()
    decode = MagicMock(return_value={"thread_id": "T1"})
    monkeypatch.setattr("kagent.core.tracing._context_attributes.read_message_metadata", decode)
    import kagent.crewai._executor as executor_module

    if hasattr(executor_module, "read_message_metadata"):
        monkeypatch.setattr(executor_module, "read_message_metadata", decode)

    attrs = _convert_a2a_request_to_span_attributes(_request_context(Part(text="hello")))

    decode.assert_not_called()
    assert "kagent.context.thread_id" not in attrs
    assert attrs["kagent.user_id"]


def test_span_attributes_do_not_override_runtime_user_id(monkeypatch):
    fake = MagicMock(return_value={"kagent.user_id": "attacker", "kagent.context.thread_id": "T1"})
    monkeypatch.setattr("kagent.core.tracing._context_attributes.caller_context_attributes", fake)
    import kagent.crewai._executor as executor_module

    if hasattr(executor_module, "caller_context_attributes"):
        monkeypatch.setattr(executor_module, "caller_context_attributes", fake)

    attrs = _convert_a2a_request_to_span_attributes(_request_context(Part(text="hello")))

    assert attrs["kagent.user_id"] != "attacker"
    assert attrs["kagent.context.thread_id"] == "T1"
