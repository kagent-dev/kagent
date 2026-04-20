"""Tests for KAgentMCPSessionManager ping-validated session caching."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from mcp.shared.exceptions import McpError
from mcp.types import ErrorData

from kagent.adk._mcp_toolset import _PING_TIMEOUT_SECONDS, KAgentMCPSessionManager


def _make_manager(**overrides):
    """Create a KAgentMCPSessionManager with a mocked connection_params."""
    params = MagicMock()
    params.url = "http://mcp.example.com/mcp"
    params.timeout = 5.0
    params.sse_read_timeout = 300.0
    params.headers = None
    return KAgentMCPSessionManager(connection_params=params, **overrides)


@pytest.mark.asyncio
async def test_create_session_returns_session_when_ping_succeeds():
    mgr = _make_manager()

    mock_session = AsyncMock()
    mock_session.send_ping = AsyncMock(return_value=None)
    mock_session.list_tools = AsyncMock(return_value=[])

    with patch.object(
        KAgentMCPSessionManager.__bases__[0],
        "create_session",
        new_callable=AsyncMock,
        return_value=mock_session,
    ) as parent_create:
        result = await mgr.create_session()

    assert result is mock_session
    mock_session.send_ping.assert_awaited_once()
    mock_session.list_tools.assert_awaited_once()
    parent_create.assert_awaited_once()


@pytest.mark.asyncio
async def test_create_session_invalidates_and_retries_when_ping_fails():
    mgr = _make_manager()

    stale_session = AsyncMock()
    stale_session.send_ping = AsyncMock(side_effect=Exception("Session terminated"))

    fresh_session = AsyncMock()
    fresh_session.send_ping = AsyncMock(return_value=None)
    fresh_session.list_tools = AsyncMock(return_value=[])

    call_count = 0

    async def _parent_create(headers=None):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            return stale_session
        return fresh_session

    with (
        patch.object(
            KAgentMCPSessionManager.__bases__[0],
            "create_session",
            side_effect=_parent_create,
        ),
        patch.object(mgr, "close", new_callable=AsyncMock) as mock_close,
    ):
        result = await mgr.create_session()

    assert result is fresh_session
    mock_close.assert_awaited_once()
    assert call_count == 2


@pytest.mark.asyncio
async def test_create_session_propagates_error_when_server_truly_down():
    mgr = _make_manager()

    with patch.object(
        KAgentMCPSessionManager.__bases__[0],
        "create_session",
        new_callable=AsyncMock,
        side_effect=ConnectionError("Failed to create MCP session"),
    ):
        with pytest.raises(ConnectionError, match="Failed to create MCP session"):
            await mgr.create_session()


@pytest.mark.asyncio
async def test_create_session_ping_respects_timeout():
    mgr = _make_manager()

    async def _slow_ping():
        await asyncio.sleep(10)

    slow_session = AsyncMock()
    slow_session.send_ping = _slow_ping

    fresh_session = AsyncMock()
    fresh_session.send_ping = AsyncMock(return_value=None)
    fresh_session.list_tools = AsyncMock(return_value=[])

    call_count = 0

    async def _parent_create(headers=None):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            return slow_session
        return fresh_session

    with (
        patch.object(
            KAgentMCPSessionManager.__bases__[0],
            "create_session",
            side_effect=_parent_create,
        ),
        patch.object(mgr, "close", new_callable=AsyncMock),
    ):
        result = await mgr.create_session()

    assert result is fresh_session
    assert call_count == 2


@pytest.mark.asyncio
async def test_create_session_accepts_method_not_found_as_alive():
    """Servers that don't implement ping reply with -32601; session is still valid."""
    mgr = _make_manager()

    mock_session = AsyncMock()
    mock_session.send_ping = AsyncMock(
        side_effect=McpError(error=ErrorData(code=-32601, message="Method not found: ping"))
    )
    mock_session.list_tools = AsyncMock(return_value=[])

    with patch.object(
        KAgentMCPSessionManager.__bases__[0],
        "create_session",
        new_callable=AsyncMock,
        return_value=mock_session,
    ) as parent_create:
        result = await mgr.create_session()

    assert result is mock_session
    mock_session.list_tools.assert_awaited_once()
    parent_create.assert_awaited_once()


@pytest.mark.asyncio
async def test_create_session_invalidates_when_list_tools_returns_session_terminated():
    """After ping passes, list_tools is used to revalidate; 404/session terminated → prune and recreate."""
    mgr = _make_manager()

    stale_session = AsyncMock()
    stale_session.send_ping = AsyncMock(return_value=None)
    stale_session.list_tools = AsyncMock(side_effect=Exception("Session terminated"))

    fresh_session = AsyncMock()
    fresh_session.send_ping = AsyncMock(return_value=None)
    fresh_session.list_tools = AsyncMock(return_value=[])

    call_count = 0

    async def _parent_create(headers=None):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            return stale_session
        return fresh_session

    with (
        patch.object(
            KAgentMCPSessionManager.__bases__[0],
            "create_session",
            side_effect=_parent_create,
        ),
        patch.object(mgr, "close", new_callable=AsyncMock) as mock_close,
    ):
        result = await mgr.create_session()

    assert result is fresh_session
    mock_close.assert_awaited_once()
    assert call_count == 2
    stale_session.list_tools.assert_awaited_once()


@pytest.mark.asyncio
async def test_create_session_invalidates_when_list_tools_returns_404():
    """list_tools returning 404 (session invalid) triggers prune and recreate."""
    mgr = _make_manager()

    stale_session = AsyncMock()
    stale_session.send_ping = AsyncMock(return_value=None)
    stale_session.list_tools = AsyncMock(side_effect=McpError(error=ErrorData(code=-32000, message="404 Not Found")))

    fresh_session = AsyncMock()
    fresh_session.send_ping = AsyncMock(return_value=None)
    fresh_session.list_tools = AsyncMock(return_value=[])

    call_count = 0

    async def _parent_create(headers=None):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            return stale_session
        return fresh_session

    with (
        patch.object(
            KAgentMCPSessionManager.__bases__[0],
            "create_session",
            side_effect=_parent_create,
        ),
        patch.object(mgr, "close", new_callable=AsyncMock) as mock_close,
    ):
        result = await mgr.create_session()

    assert result is fresh_session
    mock_close.assert_awaited_once()
    assert call_count == 2
    stale_session.list_tools.assert_awaited_once()


@pytest.mark.asyncio
async def test_create_session_propagates_non_session_error_from_list_tools():
    """Transient errors (e.g. timeout) from list_tools are propagated, not treated as session invalid."""
    mgr = _make_manager()

    mock_session = AsyncMock()
    mock_session.send_ping = AsyncMock(return_value=None)
    mock_session.list_tools = AsyncMock(side_effect=asyncio.TimeoutError())

    with patch.object(
        KAgentMCPSessionManager.__bases__[0],
        "create_session",
        new_callable=AsyncMock,
        return_value=mock_session,
    ):
        with pytest.raises(asyncio.TimeoutError):
            await mgr.create_session()


@pytest.mark.asyncio
async def test_create_session_recovers_even_when_close_raises():
    """Recovery must succeed even if close() raises during stale session teardown."""
    mgr = _make_manager()

    stale_session = AsyncMock()
    stale_session.send_ping = AsyncMock(side_effect=Exception("Session terminated"))

    fresh_session = AsyncMock()
    fresh_session.send_ping = AsyncMock(return_value=None)
    fresh_session.list_tools = AsyncMock(return_value=[])

    call_count = 0

    async def _parent_create(headers=None):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            return stale_session
        return fresh_session

    with (
        patch.object(
            KAgentMCPSessionManager.__bases__[0],
            "create_session",
            side_effect=_parent_create,
        ),
        patch.object(
            mgr,
            "close",
            new_callable=AsyncMock,
            side_effect=RuntimeError("cancel scope in different task"),
        ),
    ):
        result = await mgr.create_session()

    assert result is fresh_session
    assert call_count == 2
