"""Prefetch memory tool: loads relevant past context once at the start of a conversation."""

from __future__ import annotations

import asyncio
import logging
import re
from typing import TYPE_CHECKING

from google.adk.tools._memory_entry_utils import extract_text
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger("kagent_adk." + __name__)

# Minimum sentence length to be worth embedding on its own.
_MIN_SENTENCE_CHARS = 30

# Session state key used to mark that the prefetch already ran this invocation.
_PREFETCH_DONE_KEY = "__prefetch_memory_done__"


def _split_sentences(text: str) -> list[str]:
    """Split text into sentences using punctuation boundaries.

    Falls back to the full text as a single query if no sentence boundaries
    are found or if the message is short enough to search as-is.
    """
    sentences = re.split(r"(?<=[.!?])\s+", text.strip())
    return [s.strip() for s in sentences if len(s.strip()) >= _MIN_SENTENCE_CHARS]


class PrefetchMemoryTool(BaseTool):
    """Prefetches relevant memory once when the first user message is sent in a session.
    This is an enhanced version of the PreloadMemoryTool from ADK in `google/adk/tools/preload_memory_tool.py`.

    Runs only on the first turn (exactly one user message in the session).
    Injects past context into the LLM request so the agent has prior context without an extra tool call.

    Query strategy: split the user message into sentences and search memory for each sentence
    in parallel, then deduplicate results by memory ID. This surfaces relevant memories that
    may only match a specific part of a multi-sentence message.
    """

    def __init__(self):
        """Initialize the prefetch memory tool."""
        super().__init__(
            name="prefetch_memory",
            description="Prefetches relevant past context once at conversation start.",
        )

    @override
    async def process_llm_request(
        self,
        *,
        tool_context: ToolContext,
        llm_request: LlmRequest,
    ) -> None:
        user_content = tool_context.user_content
        if not user_content or not user_content.parts:
            return
        first_text = getattr(user_content.parts[0], "text", None) if user_content.parts else None
        if not first_text or not first_text.strip():
            return

        session = tool_context.session

        # Guard 1: only run on the first user message in the session.
        events = session.events or []
        user_message_count = sum(1 for e in events if getattr(e, "author", None) == "user")
        if user_message_count != 1:
            return

        # Guard 2: only run once per invocation — the agent may call process_llm_request
        # multiple times within a single user turn (once per LLM round-trip when taking
        # sequential tool actions), but the prefetch only needs to happen once.
        if tool_context.state.get(_PREFETCH_DONE_KEY):
            return
        tool_context.state[_PREFETCH_DONE_KEY] = True

        # Split into sentences; fall back to full text if too short to split.
        queries = _split_sentences(first_text.strip())
        if not queries:
            queries = [first_text.strip()]

        async def _search(q: str):
            try:
                return await tool_context.search_memory(q)
            except Exception:
                logger.warning("Failed to prefetch memory for query: %s", q[:100])
                return None

        # Search all sentences in parallel.
        results = await asyncio.gather(*[_search(q) for q in queries])

        # Deduplicate memories by ID, preserving first-seen order.
        seen_ids: set[str] = set()
        memory_text_lines: list[str] = []
        for response in results:
            if not response or not response.memories:
                continue
            for memory in response.memories:
                memory_id = str(getattr(memory, "id", None) or id(memory))
                if memory_id in seen_ids:
                    continue
                seen_ids.add(memory_id)
                if memory_text := extract_text(memory):
                    memory_text_lines.append(memory_text)

        if not memory_text_lines:
            return

        full_memory_text = "\n".join(memory_text_lines)
        instruction = (
            "The following content is from your previous conversations with the user. "
            "It may be useful for answering the user's current query.\n"
            "<PAST_CONVERSATIONS>\n"
            f"{full_memory_text}\n"
            "</PAST_CONVERSATIONS>\n"
        )
        llm_request.append_instructions([instruction])
