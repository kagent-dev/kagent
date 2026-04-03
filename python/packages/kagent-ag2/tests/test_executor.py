"""Basic tests for AG2AgentExecutor."""

import pytest
from unittest.mock import AsyncMock, MagicMock

from kagent.ag2._executor import AG2AgentExecutor, _extract_text


def test_extract_text_empty_parts():
    ctx = MagicMock()
    ctx.message.parts = []
    assert _extract_text(ctx) == ""


def test_extract_text_no_message():
    ctx = MagicMock()
    ctx.message = None
    result = _extract_text(ctx)
    assert result == ""


def test_executor_instantiation():
    factory = MagicMock()
    executor = AG2AgentExecutor(pattern_factory=factory, max_rounds=5)
    assert executor._max_rounds == 5
    assert executor._pattern_factory is factory


@pytest.mark.asyncio
async def test_execute_empty_message():
    factory = MagicMock()
    executor = AG2AgentExecutor(pattern_factory=factory, max_rounds=5)

    ctx = MagicMock()
    ctx.message.parts = []
    queue = AsyncMock()

    await executor.execute(ctx, queue)

    # Should have sent an error message, not called the factory
    factory.assert_not_called()
    queue.enqueue_event.assert_called()
