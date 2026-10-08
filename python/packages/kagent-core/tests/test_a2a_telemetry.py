import asyncio

import pytest
from a2a.server.agent_execution import AgentExecutor
from a2a.server.context import ServerCallContext
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import a2a_pb2 as a2a
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import SpanKind, StatusCode

from kagent.core.a2a import TelemetryRequestHandler, _telemetry
from kagent.core.telemetry import _boot
from kagent.core.telemetry._conv import KagentRuntimeValues
from kagent.core.telemetry._profiles import PROFILES
from kagent.core.telemetry._span_processor import KagentAttributesSpanProcessor

LEGACY_KEYS = {"kagent.user_id", "gen_ai.task.id", "kagent.app_name", "kagent.runtime"}


class Agent(AgentExecutor):
    def __init__(self, tracer, log, final=a2a.TASK_STATE_COMPLETED, error=None):
        self.tracer = tracer
        self.log = log
        self.final = final
        self.error = error
        self.calls = 0

    async def _status(self, context, events, state):
        await events.enqueue_event(
            a2a.TaskStatusUpdateEvent(
                task_id=context.task_id,
                context_id=context.context_id,
                status=a2a.TaskStatus(state=state),
            )
        )

    async def execute(self, context, events):
        self.calls += 1
        if context.current_task is None:
            await events.enqueue_event(
                a2a.Task(
                    id=context.task_id,
                    context_id=context.context_id,
                    status=a2a.TaskStatus(state=a2a.TASK_STATE_SUBMITTED),
                    history=[context.message],
                )
            )
        await self._status(context, events, a2a.TASK_STATE_WORKING)
        with self.tracer.start_as_current_span("model call", kind=SpanKind.CLIENT):
            pass
        if self.error is not None:
            raise self.error
        final = self.final if self.calls == 1 else a2a.TASK_STATE_COMPLETED
        await self._status(context, events, final)

    async def cancel(self, context, events):
        await self._status(context, events, a2a.TASK_STATE_CANCELED)


@pytest.fixture
def spans(monkeypatch):
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(KagentAttributesSpanProcessor())
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    monkeypatch.setattr(_telemetry, "_tracer", lambda: provider.get_tracer("kagent.core.a2a"))
    exporter.tracer = provider.get_tracer("test")
    return exporter


@pytest.fixture
def log(monkeypatch):
    entries = []

    def flush():
        entries.append("flush")

    monkeypatch.setattr(_boot, "force_flush", flush)
    return entries


def use(monkeypatch, runtime):
    identity = _boot.Identity(runtime=runtime, agent_name="researcher", agent_namespace="team-a")
    monkeypatch.setattr(_boot, "_current", _boot.Providers(identity=identity, profile=PROFILES[runtime]))


def handler_for(agent, streaming=True):
    return TelemetryRequestHandler(
        agent_executor=agent,
        task_store=InMemoryTaskStore(),
        agent_card=a2a.AgentCard(capabilities=a2a.AgentCapabilities(streaming=streaming)),
    )


def send(message_id, task_id="", context_id=""):
    return a2a.SendMessageRequest(
        message=a2a.Message(
            message_id=message_id,
            task_id=task_id,
            context_id=context_id,
            role=a2a.ROLE_USER,
            parts=[a2a.Part(text="go")],
        )
    )


def caller(user="alice"):
    return ServerCallContext(state={"headers": {"x-user-id": user}})


async def stream(handler, request, log):
    events = []
    async for event in handler.on_message_send_stream(request, caller()):
        events.append(event)
        if isinstance(event, a2a.TaskStatusUpdateEvent):
            log.append(a2a.TaskState.Name(event.status.state))
    return events


def kagent_span(exporter):
    (span,) = [span for span in exporter.get_finished_spans() if span.instrumentation_scope.name == "kagent.core.a2a"]
    return span


def assert_trace_shape(exporter):
    spans = exporter.get_finished_spans()
    ids = {span.context.span_id for span in spans}
    for span in spans:
        if span.parent is not None:
            assert span.parent.span_id in ids, f"orphan {span.name}"
        if span.kind in (SpanKind.CLIENT, SpanKind.PRODUCER):
            assert span.parent is not None, f"parentless {span.name}"
        if span.status.status_code is StatusCode.ERROR:
            assert span.status.description, f"{span.name} has no status message"


async def test_adk_turn_gets_a_transport_span_with_identity(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    agent = Agent(spans.tracer, log)
    handler = handler_for(agent)

    await stream(handler, send("m1"), log)
    await handler.aclose()

    span = kagent_span(spans)
    assert span.name == "a2a.request"
    assert span.kind is SpanKind.INTERNAL
    attributes = dict(span.attributes)
    task_id = attributes["a2a.task.id"]
    assert attributes == {
        "a2a.method": "SendStreamingMessage",
        "gen_ai.agent.name": "researcher",
        "gen_ai.agent.id": "team-a/researcher",
        "enduser.id": "alice",
        "gen_ai.conversation.id": attributes["gen_ai.conversation.id"],
        "a2a.task.id": task_id,
        "kagent.invocation.segment": "initial",
        "a2a.task.state": "TASK_STATE_COMPLETED",
    }
    assert span.status.status_code is StatusCode.UNSET

    (child,) = [span for span in spans.get_finished_spans() if span.name == "model call"]
    assert child.parent.span_id == span.context.span_id
    assert child.attributes["a2a.task.id"] == task_id
    assert child.attributes["enduser.id"] == "alice"
    assert not LEGACY_KEYS & set(child.attributes)
    assert_trace_shape(spans)


async def test_runtime_without_its_own_invoke_agent_gets_kagents(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.LANGGRAPH)
    handler = handler_for(Agent(spans.tracer, log))

    await stream(handler, send("m1"), log)
    await handler.aclose()

    span = kagent_span(spans)
    assert span.name == "invoke_agent researcher"
    assert span.attributes["gen_ai.operation.name"] == "invoke_agent"
    assert not LEGACY_KEYS & set(span.attributes)


async def test_flush_runs_before_the_quiescent_event_leaves(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    handler = handler_for(Agent(spans.tracer, log))

    await stream(handler, send("m1"), log)
    await handler.aclose()

    assert log.count("flush") == 1
    assert log.index("flush") < log.index("TASK_STATE_COMPLETED")


async def test_unary_send_ends_the_span_on_the_final_task(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    handler = handler_for(Agent(spans.tracer, log, final=a2a.TASK_STATE_INPUT_REQUIRED))

    result = await handler.on_message_send(send("m1"), caller())
    await handler.aclose()

    assert result.status.state == a2a.TASK_STATE_INPUT_REQUIRED
    span = kagent_span(spans)
    assert span.attributes["a2a.method"] == "SendMessage"
    assert span.attributes["a2a.task.state"] == "TASK_STATE_INPUT_REQUIRED"
    assert log == ["flush"]


async def test_reply_to_a_paused_task_is_a_resumed_segment(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    handler = handler_for(Agent(spans.tracer, log, final=a2a.TASK_STATE_INPUT_REQUIRED))

    first = await handler.on_message_send(send("m1"), caller())
    await handler.on_message_send(send("m2", task_id=first.id, context_id=first.context_id), caller())
    await handler.aclose()

    segments = [
        (span.attributes["kagent.invocation.segment"], span.attributes["a2a.task.state"])
        for span in spans.get_finished_spans()
        if span.instrumentation_scope.name == "kagent.core.a2a"
    ]
    assert segments == [("initial", "TASK_STATE_INPUT_REQUIRED"), ("resumed", "TASK_STATE_COMPLETED")]
    turns = [span for span in spans.get_finished_spans() if span.instrumentation_scope.name == "kagent.core.a2a"]
    children = [span for span in spans.get_finished_spans() if span.name == "model call"]
    assert [child.parent.span_id for child in children] == [turn.context.span_id for turn in turns]
    assert_trace_shape(spans)


async def test_runtime_failure_sets_error_type_and_message(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.BYO)
    handler = handler_for(Agent(spans.tracer, log, error=ValueError("provider said no")))

    with pytest.raises(Exception):  # noqa: B017 - the SDK reshapes the producer failure
        await stream(handler, send("m1"), log)
    await handler.aclose()

    span = kagent_span(spans)
    assert span.attributes["error.type"] == "runtime_error"
    assert span.status.status_code is StatusCode.ERROR
    assert span.status.description == "runtime_error: ValueError"
    assert "provider said no" not in span.status.description
    assert_trace_shape(spans)


async def test_rejected_request_ends_as_a_transport_error(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    handler = handler_for(Agent(spans.tracer, log), streaming=False)

    with pytest.raises(Exception):  # noqa: B017 - the SDK's unsupported-operation error
        await stream(handler, send("m1"), log)
    await handler.aclose()

    span = kagent_span(spans)
    assert span.attributes["error.type"] == "transport_error"
    assert span.status.description.startswith("transport_error: ")
    assert "a2a.task.id" not in span.attributes


async def test_consumer_that_leaves_marks_the_request_abandoned(monkeypatch, spans, log):
    use(monkeypatch, KagentRuntimeValues.ADK_PYTHON)
    release = asyncio.Event()

    class Waiting(Agent):
        async def execute(self, context, events):
            await events.enqueue_event(
                a2a.Task(
                    id=context.task_id,
                    context_id=context.context_id,
                    status=a2a.TaskStatus(state=a2a.TASK_STATE_SUBMITTED),
                    history=[context.message],
                )
            )
            await release.wait()
            await self._status(context, events, a2a.TASK_STATE_COMPLETED)

    handler = handler_for(Waiting(spans.tracer, log))
    events = handler.on_message_send_stream(send("m1"), caller())
    await anext(events)
    await events.aclose()
    release.set()
    await handler.aclose()

    span = kagent_span(spans)
    assert span.attributes["a2a.task.state"] == "TASK_STATE_COMPLETED"
    assert "kagent.invocation.disposition" not in span.attributes


async def test_without_bootstrap_the_span_still_carries_request_identity(monkeypatch, spans, log):
    monkeypatch.setattr(_boot, "_current", None)
    handler = handler_for(Agent(spans.tracer, log))

    await stream(handler, send("m1"), log)
    await handler.aclose()

    span = kagent_span(spans)
    assert span.name == "a2a.request"
    assert span.attributes["enduser.id"] == "alice"
    assert "flush" not in log
