from __future__ import annotations

import asyncio

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.context import ServerCallContext
from a2a.types import (
    Message,
    Part,
    Role,
    SendMessageRequest,
    Task,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from google.adk.a2a.converters.event_converter import convert_event_to_a2a_message
from google.adk.a2a.converters.long_running_functions import LongRunningFunctions
from google.adk.agents.base_agent import BaseAgent
from google.adk.events import Event
from google.adk.runners import InMemoryRunner
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import USAGE_EXTENSION_URI
from pydantic import Field

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._turn_usage import (
    TURN_USAGE_PLUGIN_NAME,
    TurnUsage,
    TurnUsagePlugin,
    attach_turn_usage,
)


def usage_event(
    prompt: int,
    completion: int,
    total: int | None = None,
    model_version: str | None = None,
    partial: bool = False,
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

    usage.add(usage_event(100, 20, 120, "model-b"))
    usage.add(usage_event(200, 30, 230, "model-a"))
    usage.add(usage_event(1, 1, 2))

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

    usage.add(usage_event(10, 5, 15))
    assert stamped_usage(usage) == counts(10, 5, 15), "models must be omitted when no event named one"


def test_counts_each_adk_event_once():
    """An event reaching the accumulator more than once is counted once."""
    usage = TurnUsage()
    event = usage_event(10, 5, 15, event_id="event-1")

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
    usage.add(usage_event(200, 30, 230, "model-b"))

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


def test_derives_total_per_call():
    """A task mixing a call that reports a total with one that does not, such
    as Anthropic's."""
    usage = TurnUsage()
    usage.add(usage_event(10, 5, 15))
    usage.add(usage_event(20, 7))

    assert stamped_usage(usage) == counts(30, 12, 42)


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


class _UsageAgent(BaseAgent):
    """Reports one call's usage, then completes, raises or blocks."""

    fail: bool = False
    block: bool = False
    reported: asyncio.Event = Field(default_factory=asyncio.Event)

    async def _run_async_impl(self, ctx):
        yield Event(
            author=self.name,
            invocation_id=ctx.invocation_id,
            model_version="model-a",
            content=genai_types.Content(role="model", parts=[genai_types.Part(text="hi")]),
            usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=10, candidates_token_count=5, total_token_count=15
            ),
        )
        self.reported.set()
        if self.fail:
            raise RuntimeError("boom")
        if self.block:
            await asyncio.Event().wait()


class _ListQueue:
    def __init__(self) -> None:
        self.events: list = []

    async def enqueue_event(self, event) -> None:
        self.events.append(event)


def _request_context(task: Task | None = None) -> RequestContext:
    message = Message(message_id="message-1", role=Role.ROLE_USER, parts=[Part(text="hi")])
    return RequestContext(
        ServerCallContext(state={}),
        SendMessageRequest(message=message),
        task_id="task-1",
        context_id="ctx-1",
        task=task,
    )


def _cancel_context(task: Task | None) -> RequestContext:
    return RequestContext(ServerCallContext(state={}), None, task_id="task-1", context_id="ctx-1", task=task)


def _completed_task_with_usage(payload) -> Task:
    task = task_with_usage(payload)
    task.status.state = TaskState.TASK_STATE_COMPLETED
    return task


def _usage_of(queue: _ListQueue, state: TaskState) -> dict:
    [event] = [e for e in queue.events if isinstance(e, TaskStatusUpdateEvent) and e.status.state == state]
    return MessageToDict(event.metadata)[USAGE_EXTENSION_URI]


_SEEDED_PLUS_ONE_CALL = {**counts(110, 25, 135), "models": [{"model": "model-a", **counts(10, 5, 15)}]}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("fail", "state"),
    [(False, TaskState.TASK_STATE_COMPLETED), (True, TaskState.TASK_STATE_FAILED)],
)
async def test_execution_end_carries_the_task_total(fail, state):
    """Runs the upstream executor, which publishes a runner failure itself
    without the after-agent interceptor."""
    runner = InMemoryRunner(agent=_UsageAgent(name="agent", fail=fail), app_name="app")
    executor = A2aAgentExecutor(runner=lambda: runner)
    queue = _ListQueue()

    await executor.execute(_request_context(_completed_task_with_usage(counts(100, 20, 120))), queue)

    assert _usage_of(queue, state) == _SEEDED_PLUS_ONE_CALL


@pytest.mark.asyncio
async def test_cancel_reports_usage_of_the_running_execution():
    """A cancel request's canceled status is the task's final event, so it
    carries the calls the interrupted execution completed."""
    agent = _UsageAgent(name="agent", block=True)
    executor = A2aAgentExecutor(runner=lambda: InMemoryRunner(agent=agent, app_name="app"))
    stored = _completed_task_with_usage(counts(100, 20, 120))
    execution = asyncio.create_task(executor.execute(_request_context(stored), _ListQueue()))
    await agent.reported.wait()

    cancel_queue = _ListQueue()
    await executor.cancel(_cancel_context(stored), cancel_queue)
    execution.cancel()
    await execution

    assert _usage_of(cancel_queue, TaskState.TASK_STATE_CANCELED) == _SEEDED_PLUS_ONE_CALL
    assert executor._running_usage == {}, "the finished execution must stop being tracked"


@pytest.mark.asyncio
async def test_cancel_during_runner_cleanup_reports_the_finished_execution():
    """The request handler holds the final event until runner cleanup ends, so
    a cancel arriving meanwhile replaces it and must carry its usage."""
    runner = InMemoryRunner(agent=_UsageAgent(name="agent"), app_name="app")
    executor = A2aAgentExecutor(runner=lambda: runner)
    stored = _completed_task_with_usage(counts(100, 20, 120))
    cleaning, release = asyncio.Event(), asyncio.Event()

    async def slow_failing_cleanup(_runner):
        cleaning.set()
        await release.wait()
        raise RuntimeError("cleanup failed")

    executor._safe_close_runner = slow_failing_cleanup
    execution = asyncio.create_task(executor.execute(_request_context(stored), _ListQueue()))
    await cleaning.wait()

    cancel_queue = _ListQueue()
    await executor.cancel(_cancel_context(stored), cancel_queue)
    release.set()
    with pytest.raises(RuntimeError, match="cleanup failed"):
        await execution

    assert _usage_of(cancel_queue, TaskState.TASK_STATE_CANCELED) == _SEEDED_PLUS_ONE_CALL
    assert executor._running_usage == {}, "a failed cleanup must still stop tracking the execution"


@pytest.mark.asyncio
async def test_cancel_of_parked_task_repeats_the_persisted_usage():
    """A task waiting for input has no running execution; its canceled status
    keeps the persisted total as the task's latest value."""
    executor = A2aAgentExecutor(runner=lambda: None)
    queue = _ListQueue()

    await executor.cancel(_cancel_context(task_with_usage(counts(100, 20, 120))), queue)

    assert _usage_of(queue, TaskState.TASK_STATE_CANCELED) == counts(100, 20, 120)


@pytest.mark.asyncio
async def test_cancel_without_usage_emits_a_bare_canceled_status():
    executor = A2aAgentExecutor(runner=lambda: None)
    queue = _ListQueue()

    await executor.cancel(_cancel_context(None), queue)

    [event] = queue.events
    assert event.status.state == TaskState.TASK_STATE_CANCELED
    assert USAGE_EXTENSION_URI not in MessageToDict(event.metadata)
