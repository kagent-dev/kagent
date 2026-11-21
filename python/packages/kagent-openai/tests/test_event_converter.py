"""Tool call and tool output conversion for the OpenAI Agents SDK runtime."""

from types import SimpleNamespace

import agents.tracing.setup as agents_tracing_setup
import pytest
from a2a.types import TaskArtifactUpdateEvent
from agents import Agent, SQLiteSession, ToolOutputText, function_tool
from agents.items import ItemHelpers, ToolCallOutputItem
from agents.models.interface import Model
from agents.stream_events import RunItemStreamEvent
from agents.tracing.provider import DefaultTraceProvider
from google.protobuf.json_format import MessageToDict
from openai.types.responses import (
    Response,
    ResponseCompletedEvent,
    ResponseFunctionToolCall,
    ResponseOutputMessage,
    ResponseOutputText,
)

from kagent.openai._agent_executor import OpenAIAgentExecutor
from kagent.openai._event_converter import convert_openai_event_to_a2a_events

CALL = ResponseFunctionToolCall(
    id="fc_1",
    call_id="call_abc123",
    name="get_pod_status",
    arguments='{"pod": "web-1"}',
    type="function_call",
)
ANSWER = ResponseOutputMessage(
    id="msg_1",
    role="assistant",
    status="completed",
    type="message",
    content=[ResponseOutputText(text="web-1 is Running", type="output_text", annotations=[])],
)


class ScriptedModel(Model):
    """Streams one scripted model response per turn."""

    def __init__(self, *turns):
        self.turns = list(turns)

    async def get_response(self, *args, **kwargs):
        raise NotImplementedError

    async def stream_response(self, *args, **kwargs):
        response = Response(
            id="resp_1",
            created_at=0,
            model="scripted",
            object="response",
            output=self.turns.pop(0),
            parallel_tool_calls=False,
            tool_choice="auto",
            tools=[],
        )
        yield ResponseCompletedEvent(type="response.completed", response=response, sequence_number=0)


class RecordingQueue:
    def __init__(self):
        self.events = []

    async def enqueue_event(self, event):
        self.events.append(event)


@function_tool
def get_pod_status(pod: str) -> str:
    """Return the status of a pod."""
    return f"{pod} is Running"


@pytest.fixture
def no_trace_export(monkeypatch):
    # A provider without processors, so the run sends no traces to api.openai.com
    monkeypatch.setattr(agents_tracing_setup, "GLOBAL_TRACE_PROVIDER", DefaultTraceProvider())


def _data_parts(events):
    return [
        MessageToDict(part.data)
        for event in events
        if isinstance(event, TaskArtifactUpdateEvent)
        for part in event.artifact.parts
        if part.HasField("data")
    ]


@pytest.mark.usefixtures("no_trace_export")
async def test_tool_result_has_the_id_and_name_of_its_call():
    agent = Agent(name="k8s", tools=[get_pod_status], model=ScriptedModel([CALL], [ANSWER]))
    executor = OpenAIAgentExecutor(agent=agent, app_name="test")
    context = SimpleNamespace(task_id="task-1", context_id="ctx-1")
    queue = RecordingQueue()

    await executor._stream_agent_events(agent, "status of web-1?", SQLiteSession("session-1"), context, queue)

    assert _data_parts(queue.events) == [
        {"id": "call_abc123", "name": "get_pod_status", "args": {"pod": "web-1"}},
        {"id": "call_abc123", "name": "get_pod_status", "response": {"result": "web-1 is Running"}},
    ]


@pytest.mark.parametrize(
    ("output", "expected"),
    [
        ({"phase": "Running", "restarts": 0}, {"phase": "Running", "restarts": 0}),
        # Not JSON data, so the result is what the SDK sent to the model
        (ToolOutputText(text="web-1 is Running"), [{"type": "input_text", "text": "web-1 is Running"}]),
    ],
)
def test_tool_output_result(output, expected):
    # Built the way the SDK builds the output item of a function tool
    raw_item = ItemHelpers.tool_call_output_item(CALL, output)
    item = ToolCallOutputItem(agent=Agent(name="k8s"), raw_item=raw_item, output=output)

    events = convert_openai_event_to_a2a_events(RunItemStreamEvent(name="tool_output", item=item), "task-1", "ctx-1")

    assert _data_parts(events) == [{"id": "call_abc123", "name": "call_abc123", "response": {"result": expected}}]
