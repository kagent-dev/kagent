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

from kagent.adk._mcp_apps import MCPAppToolNames
from kagent.adk._mcp_toolset import ConnectionSafeMcpTool, KAgentMcpToolset


def _make_mcp_tool(name, visibility=None, resource_uri=None):
    """Build a real McpTool stub whose visibility/mcp_app_resource_uri
    properties read from a fake raw MCP tool's _meta.ui block."""
    tool = McpTool.__new__(McpTool)
    tool.name = name
    ui = {}
    if visibility is not None:
        ui["visibility"] = visibility
    if resource_uri is not None:
        ui["resourceUri"] = resource_uri
    tool._mcp_tool = MagicMock(meta={"ui": ui} if ui else None)
    return tool


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
    assert "Connection reset by peer" not in result["error"]
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
    tool = _make_connection_safe_tool(McpError(ErrorData(code=-1, message="session read timeout")))

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "McpError" in result["error"]
    assert "session read timeout" not in result["error"]


@pytest.mark.asyncio
async def test_protocol_mcp_error_still_raises():
    """McpError with a protocol-level message (e.g., invalid arguments) should propagate."""
    tool = _make_connection_safe_tool(McpError(ErrorData(code=-32602, message="Invalid params: unknown tool")))

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


@pytest.mark.asyncio
async def test_get_tools_hides_app_only_tools_from_model():
    """App-only tools (_meta.ui.visibility ["app"] without "model") must not be
    exposed to the model, mirroring the Go ADK filter. Model-visible app tools
    are kept and recorded for result compaction."""
    model_app_tool = _make_mcp_tool("get_weather", visibility=["model", "app"], resource_uri="ui://w/dash")
    app_only_tool = _make_mcp_tool("refresh_dashboard", visibility=["app"], resource_uri="ui://w/dash")
    plain_tool = _make_mcp_tool("echo")  # no visibility -> model-visible by default

    app_tool_names = MCPAppToolNames()
    toolset = KAgentMcpToolset.__new__(KAgentMcpToolset)
    toolset._app_tool_names = app_tool_names

    async def mock_super_get_tools(self_arg, readonly_context=None):
        return [model_app_tool, app_only_tool, plain_tool]

    with patch.object(McpToolset, "get_tools", mock_super_get_tools):
        tools = await toolset.get_tools()

    names = {t.name for t in tools}
    assert "refresh_dashboard" not in names
    assert names == {"get_weather", "echo"}
    # Model-visible app tool is recorded; app-only tool is not.
    assert "get_weather" in app_tool_names
    assert "refresh_dashboard" not in app_tool_names


@pytest.mark.asyncio
async def test_http_status_error_returns_sanitized_error_dict_and_logs_warning():
    """HTTP status failures are returned without leaking request metadata."""
    credential = "super-secret-token"
    request = httpx.Request(
        "POST",
        "http://x",
        headers={"Authorization": f"Bearer {credential}"},
    )
    response = httpx.Response(401, request=request)
    tool = _make_connection_safe_tool(
        httpx.HTTPStatusError("Unauthorized", request=request, response=response)
    )

    with patch("kagent.adk._mcp_toolset.logger") as mock_logger:
        result = await tool.run_async(args={}, tool_context=MagicMock())

    assert isinstance(result, dict)
    assert "error" in result
    assert "401" in result["error"]
    assert credential not in result["error"]
    assert "Authorization" not in result["error"]
    mock_logger.warning.assert_called_once()
    assert credential not in str(mock_logger.warning.call_args)
    assert "Authorization" not in str(mock_logger.warning.call_args)


@pytest.mark.asyncio
async def test_mcp_error_wrapping_http_status_returns_sanitized_dict():
    """An McpError whose cause is an httpx.HTTPStatusError is reported as an HTTP
    status failure without leaking the Authorization header."""
    credential = "super-secret-token"
    request = httpx.Request(
        "POST",
        "http://x",
        headers={"Authorization": f"Bearer {credential}"},
    )
    response = httpx.Response(403, request=request)
    http_error = httpx.HTTPStatusError("Forbidden", request=request, response=response)
    mcp_error = McpError(ErrorData(code=-1, message="upstream error"))
    mcp_error.__cause__ = http_error
    tool = _make_connection_safe_tool(mcp_error)

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "403" in result["error"]
    assert credential not in result["error"]
    assert "Authorization" not in result["error"]


@pytest.mark.asyncio
async def test_exception_group_wrapping_http_status_returns_sanitized_dict():
    """An ExceptionGroup carrying an httpx.HTTPStatusError is reported as an HTTP
    status failure without leaking the Authorization header."""
    credential = "super-secret-token"
    request = httpx.Request(
        "POST",
        "http://x",
        headers={"Authorization": f"Bearer {credential}"},
    )
    response = httpx.Response(502, request=request)
    http_error = httpx.HTTPStatusError("Bad Gateway", request=request, response=response)
    group = ExceptionGroup("mcp session failed", [http_error])
    tool = _make_connection_safe_tool(group)

    result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "502" in result["error"]
    assert credential not in result["error"]
    assert "Authorization" not in result["error"]


@pytest.mark.asyncio
async def test_connection_error_response_does_not_leak_wrapped_credentials():
    """A connection error carrying an httpx request with an Authorization header
    must not leak that header via str(error) or exc_info."""
    credential = "super-secret-token"
    request = httpx.Request(
        "GET",
        "http://x",
        headers={"Authorization": f"Bearer {credential}"},
    )
    error = httpx.ConnectError("connection refused", request=request)

    tool = _make_connection_safe_tool(error)
    with patch("kagent.adk._mcp_toolset.logger") as mock_logger:
        result = await tool.run_async(args={}, tool_context=MagicMock())

    assert "error" in result
    assert "ConnectError" in result["error"]
    assert credential not in result["error"]
    assert "Authorization" not in result["error"]
    mock_logger.error.assert_called_once()
    assert mock_logger.error.call_args.kwargs.get("exc_info") is None
    assert credential not in str(mock_logger.error.call_args)
    assert "Authorization" not in str(mock_logger.error.call_args)
