from __future__ import annotations

from collections.abc import MutableMapping
from dataclasses import dataclass, fields
from typing import Any, Optional

from a2a.types import Task
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import USAGE_EXTENSION_URI

TURN_USAGE_PLUGIN_NAME = "kagent_turn_usage"


@dataclass
class TokenCounts:
    """Provider-neutral token tally, serialized with the extension field names.

    cachedInputTokens is a subset of inputTokens and outputTokens excludes
    reasoningTokens. totalTokens is the provider-reported total when there is
    one, so it can exceed the sum of the other counts.
    """

    inputTokens: int = 0
    outputTokens: int = 0
    reasoningTokens: int = 0
    cachedInputTokens: int = 0
    totalTokens: int = 0

    def add(self, other: TokenCounts) -> None:
        for name in _COUNT_FIELDS:
            setattr(self, name, getattr(self, name) + getattr(other, name))

    def is_zero(self) -> bool:
        return all(getattr(self, name) == 0 for name in _COUNT_FIELDS)

    def to_dict(self) -> dict[str, int]:
        return {name: getattr(self, name) for name in _COUNT_FIELDS}

    @classmethod
    def from_dict(cls, raw: Any) -> TokenCounts:
        if not isinstance(raw, dict):
            return cls()
        return cls(**{name: _token_count(raw.get(name)) for name in _COUNT_FIELDS})


_COUNT_FIELDS = tuple(field.name for field in fields(TokenCounts))


def call_token_counts(event: Event) -> TokenCounts:
    """Map one LLM call to provider-neutral counts. A call that reports no total
    contributes a derived one, so a task mixing providers that report a total
    with providers that do not keeps a total consistent with its parts."""
    usage = event.usage_metadata
    counts = TokenCounts(
        inputTokens=usage.prompt_token_count or 0,
        outputTokens=usage.candidates_token_count or 0,
        reasoningTokens=usage.thoughts_token_count or 0,
        cachedInputTokens=usage.cached_content_token_count or 0,
        totalTokens=usage.total_token_count or 0,
    )
    if not counts.totalTokens:
        counts.totalTokens = counts.inputTokens + counts.outputTokens + counts.reasoningTokens
    return counts


class TurnUsage:
    """Accumulates token usage across the ADK events of a task so the total can
    be emitted on terminal status updates.

    Partial (streaming chunk) events are skipped: each LLM call reports its
    usage on the final non-partial event, so summing partials would
    double-count. Events already counted are tracked by id, so an event
    reaching the accumulator more than once is counted once.
    """

    def __init__(self) -> None:
        self.total = TokenCounts()
        self.models: dict[str, TokenCounts] = {}
        self._counted_event_ids: set[str] = set()

    def add(self, event: Optional[Event]) -> None:
        if event is None or event.partial or event.usage_metadata is None:
            return
        if event.id:
            if event.id in self._counted_event_ids:
                return
            self._counted_event_ids.add(event.id)
        counts = call_token_counts(event)
        self.total.add(counts)
        if event.model_version:
            self._add_model(event.model_version, counts)

    def _add_model(self, model: str, counts: TokenCounts) -> None:
        self.models.setdefault(model, TokenCounts()).add(counts)

    def seed_from_task(self, task: Optional[Task]) -> None:
        """Prime the accumulator with the usage already persisted on a resumed
        task, so tasks spanning multiple executions (HITL input-required
        cycles, follow-up messages) report a task-lifetime total instead of the
        last segment only."""
        if task is None or not task.HasField("metadata"):
            return
        prior = MessageToDict(task.metadata).get(USAGE_EXTENSION_URI)
        if not isinstance(prior, dict):
            return
        self.total.add(TokenCounts.from_dict(prior))
        models = prior.get("models")
        for model in models if isinstance(models, list) else []:
            if isinstance(model, dict) and isinstance(model.get("model"), str) and model["model"]:
                self._add_model(model["model"], TokenCounts.from_dict(model))

    def empty(self) -> bool:
        return self.total.is_zero()

    def stamp(self, metadata: MutableMapping[str, Any]) -> None:
        """Attach the task usage to metadata under the extension URI."""
        if self.empty():
            return
        payload: dict[str, Any] = self.total.to_dict()
        if self.models:
            payload["models"] = [{"model": model, **self.models[model].to_dict()} for model in sorted(self.models)]
        metadata[USAGE_EXTENSION_URI] = payload


def _token_count(value: Any) -> int:
    """Read a numeric token count from stored task metadata; counts may be int
    or float depending on the task store's JSON round-trip."""
    if isinstance(value, bool):
        return 0
    if isinstance(value, (int, float)):
        return int(value)
    return 0


class TurnUsagePlugin(BasePlugin):
    """Feeds every ADK event the runner produces into the accumulator.

    The A2A after-event interceptor only runs for ADK events the converter
    turns into at least one A2A event. The event carrying the usage of the call
    that pauses a task for input has its long-running function call stripped
    before conversion, produces no A2A event, and would never be counted.
    """

    def __init__(self, usage: TurnUsage) -> None:
        super().__init__(name=TURN_USAGE_PLUGIN_NAME)
        self.usage = usage

    async def on_event_callback(self, *, invocation_context: InvocationContext, event: Event) -> None:
        del invocation_context
        self.usage.add(event)
        return None


def attach_turn_usage(runner: Runner, usage: TurnUsage) -> None:
    """Points the runner's usage plugin at the accumulator of this execution."""
    manager = runner.plugin_manager
    existing = manager.get_plugin(TURN_USAGE_PLUGIN_NAME)
    if isinstance(existing, TurnUsagePlugin):
        existing.usage = usage
        return
    manager.register_plugin(TurnUsagePlugin(usage))
