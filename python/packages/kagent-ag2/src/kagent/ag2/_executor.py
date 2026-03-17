"""AG2 agent executor for the A2A protocol."""

import asyncio
import logging
from collections.abc import Callable

try:
    from typing import override  # Python 3.12+
except ImportError:
    from typing_extensions import override

from a2a.server.agent_execution import AgentExecutor
from a2a.server.events import EventQueue
from a2a.server.request_handling import RequestContext
from a2a.types import (
    DataPart,
    Part,
    TaskState,
    TaskStatus,
    TextPart,
)
from a2a.utils import new_agent_text_message

from autogen.agentchat import initiate_group_chat
from autogen.agentchat.group.patterns.pattern import Pattern

logger = logging.getLogger(__name__)


def _extract_text(context: RequestContext) -> str:
    """Extract text content from an A2A request."""
    if context.message and context.message.parts:
        for part in context.message.parts:
            if isinstance(part, Part) and isinstance(
                part.root, TextPart
            ):
                return part.root.text
            if isinstance(part, TextPart):
                return part.text
    return ""


class AG2AgentExecutor(AgentExecutor):
    """Wraps an AG2 multi-agent group chat as an A2A executor.

    Args:
        pattern_factory: Callable that returns a fresh Pattern
            for each request (avoids state leakage between
            requests).
        max_rounds: Maximum conversation rounds per request.
    """

    def __init__(
        self,
        pattern_factory: Callable[[], Pattern],
        max_rounds: int = 20,
    ):
        super().__init__()
        self._pattern_factory = pattern_factory
        self._max_rounds = max_rounds

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        message = _extract_text(context)
        if not message:
            await event_queue.enqueue_event(
                new_agent_text_message(
                    "Error: No message content received."
                )
            )
            return

        # Signal that work has started
        await event_queue.enqueue_event(
            new_agent_text_message("Processing request...")
        )

        try:
            # Run AG2 group chat in a thread (sync -> async)
            pattern = self._pattern_factory()
            result, ctx, last_agent = await asyncio.to_thread(
                initiate_group_chat,
                pattern=pattern,
                messages=message,
                max_rounds=self._max_rounds,
            )

            # Extract final response
            final_message = ""
            for msg in reversed(result.chat_history):
                content = msg.get("content", "")
                if content and "TERMINATE" not in content:
                    final_message = content
                    break

            if not final_message:
                final_message = "Research complete."

            await event_queue.enqueue_event(
                new_agent_text_message(final_message)
            )

        except Exception as e:
            logger.exception("AG2 group chat failed")
            await event_queue.enqueue_event(
                new_agent_text_message(f"Error: {e}")
            )

    @override
    async def cancel(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ) -> None:
        await event_queue.enqueue_event(
            new_agent_text_message(
                "Cancellation is not supported for AG2 "
                "group chat conversations."
            )
        )
