"""Tests for the /healthz/mcp endpoint."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from starlette.testclient import TestClient

from kagent.adk._a2a import _build_mcp_health_check
from kagent.adk._mcp_toolset import KAgentMCPSessionManager
from kagent.adk.types import HttpMcpServerConfig


def _make_app(handler):
    from fastapi import FastAPI

    app = FastAPI()
    app.add_route("/healthz/mcp", methods=["GET"], route=handler)
    return app


def _make_http_tool(url="http://mcp1.example.com/mcp"):
    params = StreamableHTTPConnectionParams(url=url)
    return HttpMcpServerConfig(params=params, tools=[])


def test_no_mcp_tools_returns_ok():
    config = MagicMock()
    config.http_tools = None
    config.sse_tools = None

    handler = _build_mcp_health_check(config)
    app = _make_app(handler)

    with TestClient(app) as client:
        resp = client.get("/healthz/mcp")

    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
    assert body["servers"] == 0


def test_none_config_returns_ok():
    handler = _build_mcp_health_check(None)
    app = _make_app(handler)

    with TestClient(app) as client:
        resp = client.get("/healthz/mcp")

    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_healthy_mcp_returns_ok():
    config = MagicMock()
    config.http_tools = [_make_http_tool()]
    config.sse_tools = None

    mock_session = AsyncMock()
    mock_session.send_ping = AsyncMock(return_value=None)

    with patch.object(
        KAgentMCPSessionManager,
        "create_session",
        new_callable=AsyncMock,
        return_value=mock_session,
    ), patch.object(
        KAgentMCPSessionManager, "close", new_callable=AsyncMock
    ):
        handler = _build_mcp_health_check(config)
        app = _make_app(handler)

        with TestClient(app) as client:
            resp = client.get("/healthz/mcp")

    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
    assert body["servers"] == 1


def test_unhealthy_mcp_returns_503():
    config = MagicMock()
    config.http_tools = [_make_http_tool("http://dead-mcp.example.com/mcp")]
    config.sse_tools = None

    with patch.object(
        KAgentMCPSessionManager,
        "create_session",
        new_callable=AsyncMock,
        side_effect=ConnectionError("connection refused"),
    ), patch.object(
        KAgentMCPSessionManager, "close", new_callable=AsyncMock
    ):
        handler = _build_mcp_health_check(config)
        app = _make_app(handler)

        with TestClient(app) as client:
            resp = client.get("/healthz/mcp")

    assert resp.status_code == 503
    body = resp.json()
    assert body["status"] == "error"
    assert "http://dead-mcp.example.com/mcp" in body["errors"]


def test_method_not_found_treated_as_healthy():
    """MCP servers that don't support ping (-32601) should be reported as ok."""
    from mcp.shared.exceptions import McpError
    from mcp.types import ErrorData

    config = MagicMock()
    config.http_tools = [_make_http_tool("http://no-ping-mcp.example.com/mcp")]
    config.sse_tools = None

    with patch.object(
        KAgentMCPSessionManager,
        "create_session",
        new_callable=AsyncMock,
        side_effect=McpError(error=ErrorData(code=-32601, message="Method not found")),
    ), patch.object(
        KAgentMCPSessionManager, "close", new_callable=AsyncMock
    ):
        handler = _build_mcp_health_check(config)
        app = _make_app(handler)

        with TestClient(app) as client:
            resp = client.get("/healthz/mcp")

    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
