from __future__ import annotations

import asyncio
from unittest.mock import AsyncMock

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.context import ServerCallContext
from a2a.types import (
    Artifact,
    Message,
    Part,
    Role,
    SendMessageRequest,
    Task,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from google.adk.a2a.converters.event_converter import convert_event_to_a2a_message
from google.adk.a2a.converters.long_running_functions import LongRunningFunctions
from google.adk.a2a.converters.request_converter import AgentRunRequest
from google.adk.a2a.executor.executor_context import ExecutorContext
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.run_config import RunConfig
from google.adk.events import Event
from google.adk.runners import InMemoryRunner
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import USAGE_EXTENSION_URI

import kagent.adk._agent_executor as executor_module
from kagent.adk._agent_executor import A2aAgentExecutor, _ExecutionState
from kagent.adk._turn_usage import (
    TURN_USAGE_PLUGIN_NAME,
    TurnUsage,
    TurnUsagePlugin,
    attach_turn_usage,
)


def usage_event(
    prompt: int,
    completion: int,
    total: int,
    model_version: str | None,
    partial: bool,
    event_id: str | None = None,
) -> Event:
    event = Event(
        author="agent",
        partial=partial,
        model_version=model_version,
        usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
            prompt_token_count=prompt,
            candidates_token_count=completion,
            total_token_count=total,
        ),
    )
    if event_id is not None:
        event.id = event_id
    return event


def counts(input_tokens: int, output_tokens: int, total: int, reasoning: int = 0, cached: int = 0) -> dict:
    return {
        "inputTokens": input_tokens,
        "outputTokens": output_tokens,
        "reasoningTokens": reasoning,
        "cachedInputTokens": cached,
        "totalTokens": total,
    }


def stamped_usage(usage: TurnUsage) -> dict:
    metadata: dict = {}
    usage.stamp(metadata)
    assert USAGE_EXTENSION_URI in metadata, "usage must be stamped"
    return metadata[USAGE_EXTENSION_URI]


def task_with_usage(payload) -> Task:
    task = Task(
        id="task-1",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_INPUT_REQUIRED),
    )
    task.metadata.update({USAGE_EXTENSION_URI: payload})
    return task


def test_aggregates_non_partial_events_per_model():
    usage = TurnUsage()
    assert usage.empty()

    usage.add(usage_event(100, 20, 120, "model-b", partial=False))
    usage.add(usage_event(200, 30, 230, "model-a", partial=False))
    usage.add(usage_event(1, 1, 2, None, partial=False))

    assert not usage.empty()
    assert stamped_usage(usage) == {
        **counts(301, 51, 352),
        "models": [
            {"model": "model-a", **counts(200, 30, 230)},
            {"model": "model-b", **counts(100, 20, 120)},
        ],
    }, "each model keeps its own counts; calls naming no model count in the totals only"


def test_skips_partial_and_empty_events():
    usage = TurnUsage()

    usage.add(None)
    usage.add(Event(author="agent"))
    usage.add(usage_event(999, 999, 999, "chunk-model", partial=True))

    assert usage.empty()
    metadata: dict = {}
    usage.stamp(metadata)
    assert USAGE_EXTENSION_URI not in metadata, "empty usage must not stamp the key"

    usage.add(usage_event(10, 5, 15, None, partial=False))
    assert stamped_usage(usage) == counts(10, 5, 15), "models must be omitted when no event named one"


def test_counts_each_adk_event_once():
    """An event reaching the accumulator more than once is counted once."""
    usage = TurnUsage()
    event = usage_event(10, 5, 15, None, partial=False, event_id="event-1")

    usage.add(event)
    usage.add(event)

    assert stamped_usage(usage) == counts(10, 5, 15)


def test_payload_is_provider_neutral():
    usage = TurnUsage()
    usage.add(
        Event(
            author="agent",
            partial=False,
            usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=10,
                candidates_token_count=5,
                thoughts_token_count=3,
                cached_content_token_count=4,
                total_token_count=20,
            ),
        )
    )

    assert stamped_usage(usage) == counts(10, 5, 20, reasoning=3, cached=4)


def test_seed_from_task_accumulates_across_executions():
    usage = TurnUsage()
    usage.seed_from_task(
        task_with_usage(
            {
                **counts(100, 20, 120),
                "models": [{"model": "model-a", **counts(100, 20, 120)}],
            }
        )
    )
    usage.add(usage_event(200, 30, 230, "model-b", partial=False))

    assert stamped_usage(usage) == {
        **counts(300, 50, 350),
        "models": [
            {"model": "model-a", **counts(100, 20, 120)},
            {"model": "model-b", **counts(200, 30, 230)},
        ],
    }


def test_seed_from_task_handles_float_counts():
    usage = TurnUsage()
    usage.seed_from_task(task_with_usage({"inputTokens": 100.0, "totalTokens": 120.0}))

    assert usage.total.inputTokens == 100
    assert usage.total.totalTokens == 120


def test_seed_from_task_ignores_missing_or_malformed():
    usage = TurnUsage()
    usage.seed_from_task(None)
    usage.seed_from_task(Task(id="t", context_id="c", status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED)))
    usage.seed_from_task(task_with_usage("not-a-dict"))
    usage.seed_from_task(task_with_usage({"inputTokens": "many", "models": "nope"}))
    assert usage.empty()


def test_derives_total_when_provider_reports_none():
    """Anthropic reports input and output counts without a total."""
    usage = TurnUsage()
    usage.add(
        Event(
            author="agent",
            partial=False,
            usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=100,
                candidates_token_count=20,
            ),
        )
    )

    assert stamped_usage(usage) == counts(100, 20, 120)


def test_derives_total_per_call():
    """A task mixing a call that reports a total with one that does not."""
    usage = TurnUsage()
    usage.add(usage_event(10, 5, 15, None, partial=False))
    usage.add(
        Event(
            author="agent",
            partial=False,
            usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=20,
                candidates_token_count=7,
            ),
        )
    )

    assert stamped_usage(usage) == counts(30, 12, 42)


def test_derived_total_grows_across_executions():
    """A resumed task whose persisted total was itself derived."""
    usage = TurnUsage()
    usage.seed_from_task(task_with_usage(counts(100, 20, 120)))
    usage.add(
        Event(
            author="agent",
            partial=False,
            usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=200,
                candidates_token_count=30,
            ),
        )
    )

    assert stamped_usage(usage) == counts(300, 50, 350)


@pytest.mark.asyncio
async def test_plugin_counts_events_that_produce_no_a2a_event():
    """The event carrying the usage of the call that pauses a task has its
    long-running function call stripped before conversion, so it produces no
    A2A event and never reaches the after-event interceptor."""
    usage = TurnUsage()
    plugin = TurnUsagePlugin(usage)
    paused = Event(
        author="agent",
        partial=False,
        model_version="model-a",
        content=genai_types.Content(
            role="model",
            parts=[genai_types.Part(function_call=genai_types.FunctionCall(id="call-1", name="delete_file"))],
        ),
        long_running_tool_ids={"call-1"},
        usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
            prompt_token_count=10,
            candidates_token_count=5,
            total_token_count=15,
        ),
    )

    stripped = LongRunningFunctions(None).process_event(paused)
    assert convert_event_to_a2a_message(stripped) is None, "the stripped event must not convert to an A2A event"

    assert await plugin.on_event_callback(invocation_context=None, event=paused) is None
    assert stamped_usage(usage) == {**counts(10, 5, 15), "models": [{"model": "model-a", **counts(10, 5, 15)}]}


def test_attach_turn_usage_repoints_an_already_registered_plugin():
    runner = InMemoryRunner(agent=BaseAgent(name="agent"), app_name="app")

    first = TurnUsage()
    attach_turn_usage(runner, first)
    second = TurnUsage()
    attach_turn_usage(runner, second)

    plugin = runner.plugin_manager.get_plugin(TURN_USAGE_PLUGIN_NAME)
    assert isinstance(plugin, TurnUsagePlugin)
    assert plugin.usage is second
    assert len([p for p in runner.plugin_manager.plugins if p.name == TURN_USAGE_PLUGIN_NAME]) == 1


@pytest.mark.asyncio
async def test_executor_stamps_total_on_terminal_status_update():
    message = Message(message_id="message-1", role=Role.ROLE_USER, parts=[Part(text="hi")])
    request_context = RequestContext(
        ServerCallContext(state={}),
        SendMessageRequest(message=message),
        task_id="task-1",
        context_id="context-1",
    )
    executor = A2aAgentExecutor(runner=lambda: None)
    state = _ExecutionState(request_context=request_context)
    state.usage.seed_from_task(task_with_usage(counts(100, 20, 120)))
    executor_context = ExecutorContext(app_name="app", user_id="user-1", session_id="context-1", runner=None)

    adk_event = usage_event(10, 5, 15, "model-a", partial=False, event_id="event-1")
    a2a_event = TaskArtifactUpdateEvent(
        task_id="task-1",
        context_id="context-1",
        artifact=Artifact(artifact_id="artifact-1", parts=[Part(text="hi")]),
    )
    await TurnUsagePlugin(state.usage).on_event_callback(invocation_context=None, event=adk_event)
    assert await executor._after_event(state, executor_context, a2a_event, adk_event) is a2a_event

    terminal = TaskStatusUpdateEvent(
        task_id="task-1",
        context_id="context-1",
        status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED),
    )
    await executor._after_agent(state, executor_context, terminal)

    assert MessageToDict(terminal.metadata)[USAGE_EXTENSION_URI] == {
        **counts(110, 25, 135),
        "models": [{"model": "model-a", **counts(10, 5, 15)}],
    }


@pytest.mark.asyncio
async def test_failed_status_event_carries_the_total():
    """Failures raised outside the upstream executor publish their own terminal
    event, which must carry the total like every other terminal state."""
    message = Message(message_id="message-1", role=Role.ROLE_USER, parts=[Part(text="hi")])
    request_context = RequestContext(
        ServerCallContext(state={}),
        SendMessageRequest(message=message),
        task_id="task-1",
        context_id="context-1",
    )
    executor = A2aAgentExecutor(runner=lambda: None)
    usage = TurnUsage()
    usage.seed_from_task(task_with_usage(counts(100, 20, 120)))

    published: list[TaskStatusUpdateEvent] = []

    class _Queue:
        async def enqueue_event(self, event):
            published.append(event)

    await executor._publish_failed_status_event(request_context, _Queue(), "boom", usage)

    assert len(published) == 1
    assert published[0].status.state == TaskState.TASK_STATE_FAILED
    assert MessageToDict(published[0].metadata)[USAGE_EXTENSION_URI] == counts(100, 20, 120)


class _ListQueue:
    def __init__(self) -> None:
        self.events: list = []

    async def enqueue_event(self, event) -> None:
        self.events.append(event)


def _cancel_context(task: Task | None) -> RequestContext:
    return RequestContext(
        ServerCallContext(state={}),
        None,
        task_id="task-1",
        context_id="ctx-1",
        task=task,
    )


def _canceled_usage(queue: _ListQueue) -> dict:
    [event] = [e for e in queue.events if e.status.state == TaskState.TASK_STATE_CANCELED]
    return MessageToDict(event.metadata)[USAGE_EXTENSION_URI]


@pytest.mark.asyncio
async def test_cancel_reports_usage_of_the_running_execution(monkeypatch):
    """A cancel request's canceled status is the task's final event, so it
    carries the calls the interrupted execution completed."""
    runner = InMemoryRunner(agent=BaseAgent(name="agent"), app_name="app")
    executor = A2aAgentExecutor(runner=lambda: None)
    executor._resolve_runner = AsyncMock(return_value=runner)
    executor._convert_request = lambda request_context, part_converter: AgentRunRequest(
        user_id="user-1", session_id="ctx-1", run_config=RunConfig()
    )
    executor._prepare_session = AsyncMock()
    executor._safe_close_runner = AsyncMock()
    counted = asyncio.Event()

    class BlockingUpstreamExecutor:
        def __init__(self, *, runner, config, force_new_version):
            self.runner = runner

        async def execute(self, request_context, queue):
            plugin = self.runner.plugin_manager.get_plugin(TURN_USAGE_PLUGIN_NAME)
            await plugin.on_event_callback(
                invocation_context=None, event=usage_event(10, 5, 15, "model-a", partial=False, event_id="e1")
            )
            counted.set()
            await asyncio.Event().wait()

    monkeypatch.setattr(executor_module, "UpstreamA2aAgentExecutor", BlockingUpstreamExecutor)
    stored = task_with_usage(counts(100, 20, 120))
    message = Message(message_id="message-1", role=Role.ROLE_USER, parts=[Part(text="hi")])
    execution_queue = _ListQueue()
    execution = asyncio.create_task(
        executor.execute(
            RequestContext(
                ServerCallContext(state={}),
                SendMessageRequest(message=message),
                task_id="task-1",
                context_id="ctx-1",
                task=stored,
            ),
            execution_queue,
        )
    )
    await counted.wait()

    cancel_queue = _ListQueue()
    await executor.cancel(_cancel_context(stored), cancel_queue)
    execution.cancel()
    await execution

    assert _canceled_usage(cancel_queue) == {
        **counts(110, 25, 135),
        "models": [{"model": "model-a", **counts(10, 5, 15)}],
    }
    assert executor._running_usage == {}, "the finished execution must stop being tracked"


@pytest.mark.asyncio
async def test_cancel_of_parked_task_repeats_the_persisted_usage():
    """A task waiting for input has no running execution; its canceled status
    keeps the persisted total as the task's latest value."""
    executor = A2aAgentExecutor(runner=lambda: None)
    queue = _ListQueue()

    await executor.cancel(_cancel_context(task_with_usage(counts(100, 20, 120))), queue)

    assert _canceled_usage(queue) == counts(100, 20, 120)


@pytest.mark.asyncio
async def test_cancel_without_usage_emits_a_bare_canceled_status():
    executor = A2aAgentExecutor(runner=lambda: None)
    queue = _ListQueue()

    await executor.cancel(_cancel_context(None), queue)

    [event] = queue.events
    assert event.status.state == TaskState.TASK_STATE_CANCELED
    assert USAGE_EXTENSION_URI not in MessageToDict(event.metadata)
