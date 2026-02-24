"""Prefetch memory tool: loads relevant past context once at the start of a conversation."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from google.adk.tools._memory_entry_utils import extract_text
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger("kagent_adk." + __name__)


class PrefetchMemoryTool(BaseTool):
    """Prefetches relevant memory once when the first user message is sent in a session.
    This is an enhanced version of the PreloadMemoryTool from ADK in `google/adk/tools/preload_memory_tool.py`.

    Runs only on the first turn (exactly one user message in the session).
    Injects past context into the LLM request so the agent has prior context without an extra tool call.

    Query strategy: use the entire first user message as the search query. Simple and good for short messages.
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
        events = session.events or []
        user_message_count = sum(1 for e in events if getattr(e, "author", None) == "user")
        if user_message_count != 1:
            return

        query: str
        query = first_text.strip()

        try:
            # TODO: Maybe we can split it into sentences and search for each sentence?
            # TODO: Maybe we can use the agent's model to generate a short search query from the user message before searching memory?
            response = await tool_context.search_memory(query)
        except Exception:
            logger.warning("Failed to prefetch memory for query: %s", query[:100])
            return

        if not response.memories:
            return

        memory_text_lines = []
        for memory in response.memories:
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

    # TODO: DEPRECATED
    async def _derive_search_query(self, tool_context: ToolContext, user_message: str) -> str | None:
        """Use the agent's model to produce a short search query from the user message."""
        inv = getattr(tool_context, "_invocation_context", None)
        agent = getattr(inv, "agent", None) if inv else None
        model = getattr(agent, "model", None) if agent else None
        if not model:
            return None
        try:
            from google.adk.models.llm_request import LlmRequest
            from google.genai.types import Content, Part

            prompt = (
                "Given the user message below, output a single short search query (one line) "
                "for finding relevant past context. Output only the query, no explanation.\n\n"
                f"User message:\n{user_message[:2000]}\n\nSearch query:"
            )
            llm_request = LlmRequest(
                contents=[Content(role="user", parts=[Part(text=prompt)])],
            )
            gen = model.generate_content_async(llm_request, stream=False)
            text_parts = []
            async for chunk in gen:
                if chunk.content and chunk.content.parts:
                    for part in chunk.content.parts:
                        if getattr(part, "text", None):
                            text_parts.append(part.text)
            return " ".join(text_parts).strip() if text_parts else None
        except Exception as e:
            logger.debug("LLM-derived search query failed: %s", e)
            return None
