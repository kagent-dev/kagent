from a2a.types import Task, TaskState, TaskStatus
from google.adk.events import Event
from google.genai import types as genai_types

from kagent.adk._turn_usage import USAGE_TOTAL_KEY, TurnUsage
from kagent.adk.converters.event_converter import serialize_metadata_value


def usage_event(prompt: int, completion: int, total: int, model_version: str | None, partial: bool) -> Event:
    return Event(
        author="agent",
        partial=partial,
        model_version=model_version,
        usage_metadata=genai_types.GenerateContentResponseUsageMetadata(
            prompt_token_count=prompt,
            candidates_token_count=completion,
            total_token_count=total,
        ),
    )


def stamped_total(usage: TurnUsage) -> dict:
    metadata: dict = {}
    usage.stamp(metadata)
    assert USAGE_TOTAL_KEY in metadata, "kagent_usage_total must be stamped"
    return metadata[USAGE_TOTAL_KEY]


def task_with_total(total: dict) -> Task:
    return Task(
        id="task-1",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.input_required),
        metadata={USAGE_TOTAL_KEY: total},
    )


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
    usage.seed_from_task(Task(id="t", context_id="c", status=TaskStatus(state=TaskState.completed)))
    usage.seed_from_task(task_with_total("not-a-dict"))  # type: ignore[arg-type]
    assert usage.empty()
