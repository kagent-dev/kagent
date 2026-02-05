import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest

from kagent.adk._safe_request_handler import SafeRequestHandler


@pytest.mark.asyncio
async def test_cleanup_producer_swallows_cancelled_error():
    mock_queue_manager = AsyncMock()
    handler = SafeRequestHandler(
        agent_executor=AsyncMock(),
        task_store=AsyncMock(),
        request_context_builder=AsyncMock(),
    )
    handler._queue_manager = mock_queue_manager
    handler._running_agents = {"task-123": MagicMock()}
    handler._running_agents_lock = asyncio.Lock()

    async def cancelled_task():
        raise asyncio.CancelledError()

    producer_task = asyncio.create_task(cancelled_task())
    try:
        await handler._cleanup_producer(producer_task, task_id="task-123")
    finally:
        if not producer_task.done():
            producer_task.cancel()

    # Verify cleanup was still performed despite cancellation
    mock_queue_manager.close.assert_called_once_with("task-123")
    assert "task-123" not in handler._running_agents


@pytest.mark.asyncio
async def test_cleanup_producer_normal_completion():
    """Verify normal cleanup calls parent correctly."""
    mock_queue_manager = AsyncMock()
    handler = SafeRequestHandler(
        agent_executor=AsyncMock(),
        task_store=AsyncMock(),
        request_context_builder=AsyncMock(),
    )
    handler._queue_manager = mock_queue_manager
    handler._running_agents = {"task-456": MagicMock()}
    handler._running_agents_lock = asyncio.Lock()

    async def successful_task():
        return "done"

    producer_task = asyncio.create_task(successful_task())
    await handler._cleanup_producer(producer_task, task_id="task-456")

    # Verify cleanup was performed
    mock_queue_manager.close.assert_called_once_with("task-456")
    assert "task-456" not in handler._running_agents
