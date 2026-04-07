"""Tests for ConnectionSafeMcpTool — connection errors are returned as
error text to the LLM instead of raised, preventing tight retry loops.

See: https://github.com/kagent-dev/kagent/issues/1530
"""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest
from google.adk.tools.mcp_tool.mcp_tool import McpTool
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset
from mcp.shared.exceptions import McpError
from mcp.types import ErrorData

from kagent.adk._mcp_toolset import ConnectionSafeMcpTool, KAgentMcpToolset


def _make_connection_safe_tool(side_effect):
    """Create a ConnectionSafeMcpTool wrapping a mock McpTool."""
    inner_tool = MagicMock(spec=McpTool)
    inner_tool.name = "test-tool"
    inner_tool.run_async = AsyncMock(side_effect=side_effect)
    return ConnectionSafeMcpTool(inner_tool)


@pytest.mark.asyncio
async def test_connection_reset_error_returns_error_dict():
    """ConnectionResetError should be caught and returned as error text."""
    tool = _make_connection_safe_tool(ConnectionResetError("Connection reset by peer"))

    result = await tool.run_async(args={"key": "value"}, tool_context=MagicMock())

    assert "error" in result
    assert "ConnectionResetError" in result["error"]
    assert "Connection reset by peer" in result["error"]
    assert "Do not retry" in result["error"]


@pytest.mark.asyncio
async def test_connection_refused_error_returns_error_dict():
    """ConnectionRefusedError should be caught and returned as error text."""
    tool = _make_connection_safe_tool(ConnectionRefusedError("Connection refused"))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "ConnectionRefusedError" in result["error"]


@pytest.mark.asyncio
async def test_timeout_error_returns_error_dict():
    """TimeoutError should be caught and returned as error text."""
    tool = _make_connection_safe_tool(TimeoutError("timed out"))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "TimeoutError" in result["error"]


@pytest.mark.asyncio
async def test_httpx_connect_error_returns_error_dict():
    """httpx.ConnectError should be caught via httpx.TransportError."""
    tool = _make_connection_safe_tool(httpx.ConnectError("connection refused"))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "ConnectError" in result["error"]


@pytest.mark.asyncio
async def test_httpx_read_error_returns_error_dict():
    """httpx.ReadError (connection reset by peer) should be caught."""
    tool = _make_connection_safe_tool(httpx.ReadError("peer closed connection"))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "ReadError" in result["error"]


@pytest.mark.asyncio
async def test_httpx_connect_timeout_returns_error_dict():
    """httpx.ConnectTimeout should be caught via httpx.TransportError."""
    tool = _make_connection_safe_tool(httpx.ConnectTimeout("timed out"))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "ConnectTimeout" in result["error"]


@pytest.mark.asyncio
async def test_transport_mcp_error_returns_error_dict():
    """McpError with a transport-level message (e.g., session read timeout) should be caught."""
    tool = _make_connection_safe_tool(
        McpError(ErrorData(code=-1, message="session read timeout"))
    )

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "McpError" in result["error"]
    assert "session read timeout" in result["error"]


@pytest.mark.asyncio
async def test_protocol_mcp_error_still_raises():
    """McpError with a protocol-level message (e.g., invalid arguments) should propagate."""
    tool = _make_connection_safe_tool(
        McpError(ErrorData(code=-32602, message="Invalid params: unknown tool"))
    )

    with pytest.raises(McpError, match="Invalid params"):
        await tool.run_async(args={}, tool_context=MagicMock())


@pytest.mark.asyncio
async def test_non_connection_error_still_raises():
    """Non-connection errors (e.g. ValueError) should still propagate."""
    tool = _make_connection_safe_tool(ValueError("bad argument"))

    with pytest.raises(ValueError, match="bad argument"):
        await tool.run_async(args={}, tool_context=MagicMock())


@pytest.mark.asyncio
async def test_cancelled_error_still_raises():
    """CancelledError must propagate — it's not a connection error."""
    tool = _make_connection_safe_tool(asyncio.CancelledError("cancelled"))

    with pytest.raises(asyncio.CancelledError):
        await tool.run_async(args={}, tool_context=MagicMock())


@pytest.mark.asyncio
async def test_get_tools_wraps_mcp_tools():
    """KAgentMcpToolset.get_tools should wrap McpTool instances with ConnectionSafeMcpTool."""
    fake_mcp_tool = McpTool.__new__(McpTool)
    fake_mcp_tool.name = "wrapped-tool"
    fake_mcp_tool._some_attr = "value"

    fake_other_tool = MagicMock()
    fake_other_tool.name = "other-tool"

    toolset = KAgentMcpToolset.__new__(KAgentMcpToolset)

    async def mock_super_get_tools(self_arg, readonly_context=None):
        return [fake_mcp_tool, fake_other_tool]

    with patch.object(McpToolset, "get_tools", mock_super_get_tools):
        tools = await toolset.get_tools()

    assert len(tools) == 2
    assert isinstance(tools[0], ConnectionSafeMcpTool)
    assert tools[0].name == "wrapped-tool"
    assert tools[0]._some_attr == "value"
    assert tools[1] is fake_other_tool
