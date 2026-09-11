from __future__ import annotations

from typing import Any, Optional

from a2a.types import Task
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.genai import types as genai_types
from kagent.core.a2a import get_kagent_metadata_key

from .converters.event_converter import serialize_metadata_value

# Every terminal status update of a task carries the running task-lifetime
# total, so consumers take the latest value rather than summing across
# executions.
USAGE_TOTAL_KEY = get_kagent_metadata_key("usage_total")

TURN_USAGE_PLUGIN_NAME = "kagent_turn_usage"


class TurnUsage:
    """Accumulates token usage across the ADK events of one execution so the
    aggregated total can be emitted on terminal status updates.

    Partial (streaming chunk) events are skipped: each LLM call reports its
    usage on the final non-partial event, so summing partials would
    double-count. Events already counted are tracked by id, so an event
    reaching the accumulator more than once is counted once.
    """

    def __init__(self) -> None:
        self.prompt_tokens = 0
        self.completion_tokens = 0
        self.thoughts_tokens = 0
        self.cached_content_tokens = 0
        self.total_tokens = 0
        self.model_version: Optional[str] = None
        self._counted_event_ids: set[str] = set()

    def add(self, event: Optional[Event]) -> None:
        if event is None or event.partial:
            return
        usage = event.usage_metadata
        if usage is None:
            return
        if event.id:
            if event.id in self._counted_event_ids:
                return
            self._counted_event_ids.add(event.id)
        self.prompt_tokens += usage.prompt_token_count or 0
        self.completion_tokens += usage.candidates_token_count or 0
        self.thoughts_tokens += usage.thoughts_token_count or 0
        self.cached_content_tokens += usage.cached_content_token_count or 0
        self.total_tokens += usage.total_token_count or 0
        if event.model_version:
            self.model_version = event.model_version

    def seed_from_task(self, task: Optional[Task]) -> None:
        """Prime the accumulator with the total already persisted on a resumed
        task, so tasks spanning multiple executions (HITL input-required
        cycles, follow-up messages) report a task-lifetime total instead of the
        last segment only."""
        if task is None or not task.metadata or USAGE_TOTAL_KEY not in task.metadata:
            return
        prior = task.metadata[USAGE_TOTAL_KEY]
        if not hasattr(prior, "items"):
            return
        prior = dict(prior.items())
        self.prompt_tokens += _token_count(prior.get("promptTokenCount"))
        self.completion_tokens += _token_count(prior.get("candidatesTokenCount"))
        self.thoughts_tokens += _token_count(prior.get("thoughtsTokenCount"))
        self.cached_content_tokens += _token_count(prior.get("cachedContentTokenCount"))
        self.total_tokens += _token_count(prior.get("totalTokenCount"))
        model_version = prior.get("modelVersion")
        if isinstance(model_version, str) and model_version:
            self.model_version = model_version

    def empty(self) -> bool:
        return self.prompt_tokens == 0 and self.completion_tokens == 0 and self.total_tokens == 0

    def total_token_count(self) -> int:
        """Anthropic and other providers report per-call input and output counts
        without a total, so derive one rather than emitting a zero total next to
        non-zero counts."""
        if self.total_tokens:
            return self.total_tokens
        return self.prompt_tokens + self.completion_tokens + self.thoughts_tokens

    def stamp(self, metadata: dict[str, Any]) -> None:
        """Attach the aggregate to metadata under kagent_usage_total. The value
        is serialized exactly like the per-event kagent_usage_metadata (same
        genai type, same serializer) plus modelVersion, so consumers can share
        one parser."""
        if self.empty():
            return
        total = serialize_metadata_value(
            genai_types.GenerateContentResponseUsageMetadata(
                prompt_token_count=self.prompt_tokens or None,
                candidates_token_count=self.completion_tokens or None,
                thoughts_token_count=self.thoughts_tokens or None,
                cached_content_token_count=self.cached_content_tokens or None,
                total_token_count=self.total_token_count() or None,
            )
        )
        if not isinstance(total, dict):
            return
        if self.model_version:
            total["modelVersion"] = self.model_version
        metadata[USAGE_TOTAL_KEY] = total


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
