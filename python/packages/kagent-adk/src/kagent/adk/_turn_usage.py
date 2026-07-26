from __future__ import annotations

from typing import Any, Optional

from a2a.types import Task
from google.adk.events import Event
from google.genai import types as genai_types
from kagent.core.a2a import get_kagent_metadata_key

from .converters.event_converter import serialize_metadata_value

USAGE_TOTAL_KEY = get_kagent_metadata_key("usage_total")


class TurnUsage:
    """Accumulates token usage across the ADK events of one execution so the
    aggregated total can be emitted on terminal status updates.

    Partial (streaming chunk) events are skipped: each LLM call reports its
    usage on the final non-partial event, so summing partials would
    double-count.
    """

    def __init__(self) -> None:
        self.prompt_tokens = 0
        self.completion_tokens = 0
        self.thoughts_tokens = 0
        self.cached_content_tokens = 0
        self.total_tokens = 0
        self.model_version: Optional[str] = None

    def add(self, event: Optional[Event]) -> None:
        if event is None or event.partial:
            return
        usage = event.usage_metadata
        if usage is None:
            return
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
        if task is None or not task.metadata:
            return
        prior = task.metadata.get(USAGE_TOTAL_KEY)
        if not isinstance(prior, dict):
            return
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
                total_token_count=self.total_tokens or None,
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
