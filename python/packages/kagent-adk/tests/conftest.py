"""Test configuration and fixtures for kagent-adk tests."""

import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest


@pytest.fixture(scope="session")
def event_loop():
    """Create an instance of the default event loop for the test session."""
    loop = asyncio.get_event_loop_policy().new_event_loop()
    yield loop
    loop.close()


@pytest.fixture
def mock_httpx_client():
    """Mock httpx.AsyncClient for testing."""
    client = AsyncMock()
    client.post = AsyncMock()
    client.get = AsyncMock()
    client.delete = AsyncMock()
    return client


@pytest.fixture
def mock_request_context():
    """Mock A2A request context for testing."""
    context = MagicMock()
    context.task_id = "test-task-123"
    context.context_id = "test-context-456"
    context.message = MagicMock()
    context.current_task = None
    context.call_context = MagicMock()
    context.call_context.request = MagicMock()
    context.call_context.request.headers = {}
    return context


@pytest.fixture
def mock_event_queue():
    """Mock A2A event queue for testing."""
    queue = AsyncMock()
    queue.enqueue_event = AsyncMock()
    return queue
