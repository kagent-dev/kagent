from __future__ import annotations

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
from google.adk.a2a.executor.executor_context import ExecutorContext
from google.adk.events import Event
from google.genai import types as genai_types

from kagent.adk._agent_executor import A2aAgentExecutor, _ExecutionState
from kagent.adk._turn_usage import USAGE_TOTAL_KEY, TurnUsage
from kagent.adk.converters.event_converter import serialize_metadata_value


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


def stamped_total(usage: TurnUsage) -> dict:
    metadata: dict = {}
    usage.stamp(metadata)
    assert USAGE_TOTAL_KEY in metadata, "kagent_usage_total must be stamped"
    return metadata[USAGE_TOTAL_KEY]


def task_with_total(total) -> Task:
    task = Task(
        id="task-1",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_INPUT_REQUIRED),
    )
    task.metadata.update({USAGE_TOTAL_KEY: total})
    return task


def test_aggregates_non_partial_events():
    usage = TurnUsage()
    assert usage.empty()

    usage.add(usage_event(100, 20, 120, "model-a", partial=False))
    usage.add(usage_event(200, 30, 230, "model-b", partial=False))

    assert not usage.empty()
    assert stamped_total(usage) == {
        "promptTokenCount": 300,
        "candidatesTokenCount": 50,
        "totalTokenCount": 350,
        "modelVersion": "model-b",
    }


def test_skips_partial_and_empty_events():
    usage = TurnUsage()

    usage.add(None)
    usage.add(Event(author="agent"))
    usage.add(usage_event(999, 999, 999, "chunk-model", partial=True))

    assert usage.empty()
    metadata: dict = {}
    usage.stamp(metadata)
    assert USAGE_TOTAL_KEY not in metadata, "empty usage must not stamp the key"

    usage.add(usage_event(10, 5, 15, None, partial=False))
    assert stamped_total(usage) == {
        "promptTokenCount": 10,
        "candidatesTokenCount": 5,
        "totalTokenCount": 15,
    }, "modelVersion must be omitted when no event carried one"


def test_counts_each_adk_event_once():
    """One ADK event can be converted into several A2A events, and the
    after-event interceptor runs for each of them."""
    usage = TurnUsage()
    event = usage_event(10, 5, 15, None, partial=False, event_id="event-1")

    usage.add(event)
    usage.add(event)

    assert stamped_total(usage) == {
        "promptTokenCount": 10,
        "candidatesTokenCount": 5,
        "totalTokenCount": 15,
    }


def test_shape_matches_per_event_usage_metadata():
    event = usage_event(10, 5, 15, None, partial=False)

    usage = TurnUsage()
    usage.add(event)

    assert stamped_total(usage) == serialize_metadata_value(event.usage_metadata), (
        "kagent_usage_total must serialize identically to kagent_usage_metadata"
    )


def test_seed_from_task_accumulates_across_executions():
    usage = TurnUsage()
    usage.seed_from_task(
        task_with_total(
            {
                "promptTokenCount": 100,
                "candidatesTokenCount": 20,
                "totalTokenCount": 120,
                "modelVersion": "model-a",
            }
        )
    )
    usage.add(usage_event(200, 30, 230, None, partial=False))

    assert stamped_total(usage) == {
        "promptTokenCount": 300,
        "candidatesTokenCount": 50,
        "totalTokenCount": 350,
        "modelVersion": "model-a",
    }


def test_seed_from_task_handles_float_counts():
    usage = TurnUsage()
    usage.seed_from_task(task_with_total({"promptTokenCount": 100.0, "totalTokenCount": 120.0}))

    assert usage.prompt_tokens == 100
    assert usage.total_tokens == 120


def test_seed_from_task_ignores_missing_or_malformed():
    usage = TurnUsage()
    usage.seed_from_task(None)
    usage.seed_from_task(Task(id="t", context_id="c", status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED)))
    usage.seed_from_task(task_with_total("not-a-dict"))
    assert usage.empty()


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
    state.usage.seed_from_task(
        task_with_total({"promptTokenCount": 100, "candidatesTokenCount": 20, "totalTokenCount": 120})
    )
    executor_context = ExecutorContext(app_name="app", user_id="user-1", session_id="context-1", runner=None)

    adk_event = usage_event(10, 5, 15, "model-a", partial=False, event_id="event-1")
    a2a_event = object()
    for _ in range(2):
        assert await executor._after_event(state, executor_context, a2a_event, adk_event) is a2a_event

    terminal = TaskStatusUpdateEvent(
        task_id="task-1",
        context_id="context-1",
        status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED),
    )
    await executor._after_agent(state, executor_context, terminal)

    total = dict(terminal.metadata[USAGE_TOTAL_KEY].items())
    assert total == {
        "promptTokenCount": 110,
        "candidatesTokenCount": 25,
        "totalTokenCount": 135,
        "modelVersion": "model-a",
    }
