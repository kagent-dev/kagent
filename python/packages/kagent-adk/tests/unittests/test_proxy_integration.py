import json
import socket
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any

import httpx
import pytest
from google.adk.agents.remote_a2a_agent import AGENT_CARD_WELL_KNOWN_PATH

from kagent.adk.types import PROXY_HOST_HEADER, AgentConfig, OpenAI, RemoteAgentConfig


class RequestRecordingHandler(BaseHTTPRequestHandler):
    """HTTP handler that records all incoming requests."""

    requests_received = []

    def do_GET(self):
        """Handle GET requests."""
        self.requests_received.append(
            {
                "method": self.command,
                "path": self.path,
                "headers": dict(self.headers),
            }
        )
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        # Return a mock agent card response
        response = {
            "name": "remote_agent",
            "description": "Remote agent",
            "url": "http://remote-agent.kagent:8080",
            "capabilities": {"streaming": True},
            "skills": [],
        }
        self.wfile.write(json.dumps(response).encode())

    def do_POST(self):
        """Handle POST requests."""
        self.do_GET()  # Same handling for now

    def log_message(self, format, *args):
        """Suppress log messages."""
        pass


class TestHTTPServer:
    """Context manager for running a test HTTP server that records requests."""

    def __init__(self, port: int = 0):
        self.port = port
        self.server: HTTPServer | None = None
        self.thread: threading.Thread | None = None
        # Clear requests before starting
        RequestRecordingHandler.requests_received = []

    def __enter__(self) -> "TestHTTPServer":
        """Start the HTTP server in a background thread."""
        # Find an available port if port is 0
        if self.port == 0:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.bind(("", 0))
                self.port = s.getsockname()[1]

        self.server = HTTPServer(("localhost", self.port), RequestRecordingHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

        # Wait for server to be ready
        time.sleep(0.1)

        return self

    def __exit__(self, *args: Any) -> None:
        """Shutdown the HTTP server."""
        if self.server:
            self.server.shutdown()
            self.server.server_close()
        if self.thread:
            self.thread.join(timeout=1.0)

    @property
    def url(self) -> str:
        """Get the base URL of the test server."""
        return f"http://localhost:{self.port}"

    @property
    def requests(self) -> list[dict]:
        """Get all requests received by the server."""
        return RequestRecordingHandler.requests_received


@pytest.mark.asyncio
async def test_remote_agent_with_proxy_url():
    """Test that RemoteA2aAgent requests go through the proxy URL with correct proxy host header.

    When proxy is configured, requests should be made to the proxy URL (our test server)
    with the proxy host header set for proxy routing. This test uses a real HTTP server
    to verify actual request behavior.
    """
    with TestHTTPServer() as test_server:
        config = AgentConfig(
            model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
            description="Test agent",
            instruction="You are a test agent",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote_agent",
                    url=test_server.url,  # Use test server as proxy URL
                    description="Remote agent",
                    headers={PROXY_HOST_HEADER: "remote-agent.kagent"},  # Proxy host header for proxy routing
                )
            ],
        )

        agent = config.to_agent("test_agent")

        # Find the RemoteA2aAgent tool
        from google.adk.tools.agent_tool import AgentTool

        remote_agent_tool = None
        for tool in agent.tools:
            if isinstance(tool, AgentTool):
                remote_agent_tool = tool.agent
                break

        assert remote_agent_tool is not None

        # Make a request - this should go through the proxy (test server)
        # The client has base_url set to the proxy, so we can use a relative path
        async with remote_agent_tool._httpx_client as client:
            await client.get(AGENT_CARD_WELL_KNOWN_PATH)

        # Verify that requests were made to the proxy URL (test server)
        assert len(test_server.requests) > 0, "No requests were received by test server"
        request = test_server.requests[0]
        assert request["path"] == AGENT_CARD_WELL_KNOWN_PATH
        # Verify proxy host header is set for proxy routing
        assert (
            request["headers"].get(PROXY_HOST_HEADER) == "remote-agent.kagent"
            or request["headers"].get(PROXY_HOST_HEADER.lower()) == "remote-agent.kagent"
        )


def test_remote_agent_no_proxy_when_not_configured():
    """Test that RemoteA2aAgent HTTP client works without proxy."""
    config = AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        remote_agents=[
            RemoteAgentConfig(
                name="remote_agent",
                url="http://remote-agent:8080",
                description="Remote agent",
            )
        ],
    )

    agent = config.to_agent("test_agent")

    # Find the RemoteA2aAgent tool
    # AgentTool wraps the RemoteA2aAgent
    remote_agent_tool = None
    for tool in agent.tools:
        if hasattr(tool, "agent"):
            remote_agent_tool = tool.agent
            break

    assert remote_agent_tool is not None, (
        f"No RemoteA2aAgent tool found. Tools: {[type(t).__name__ for t in agent.tools]}"
    )

    # Verify agent was created successfully (no proxy configuration means no special setup needed)
    assert remote_agent_tool.name == "remote_agent"


@pytest.mark.asyncio
async def test_remote_agent_direct_url_no_proxy():
    """Test that RemoteA2aAgent makes requests to direct URL when no proxy is configured."""
    with TestHTTPServer() as test_server:
        config = AgentConfig(
            model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
            description="Test agent",
            instruction="You are a test agent",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote_agent",
                    url=test_server.url,  # Direct URL (no proxy)
                    description="Remote agent",
                )
            ],
        )

        agent = config.to_agent("test_agent")

        # Find the RemoteA2aAgent tool
        from google.adk.tools.agent_tool import AgentTool

        remote_agent_tool = None
        for tool in agent.tools:
            if isinstance(tool, AgentTool):
                remote_agent_tool = tool.agent
                break

        assert remote_agent_tool is not None

        # Make a request - should go directly to the configured URL
        # When no proxy is configured, we need to use the full URL
        async with remote_agent_tool._httpx_client as client:
            await client.get(f"{test_server.url}{AGENT_CARD_WELL_KNOWN_PATH}")

        # Verify request went to direct URL (no proxy)
        assert len(test_server.requests) > 0
        assert test_server.requests[0]["path"] == AGENT_CARD_WELL_KNOWN_PATH
        # Verify Host header is set automatically by httpx based on URL
        # (proxy host header should not be present when no proxy is configured)
        headers = test_server.requests[0]["headers"]
        assert (
            headers.get("Host") == f"localhost:{test_server.port}"
            or headers.get("host") == f"localhost:{test_server.port}"
        )
        assert PROXY_HOST_HEADER not in headers and PROXY_HOST_HEADER.lower() not in headers


@pytest.mark.asyncio
async def test_remote_agent_with_headers():
    """Test that RemoteA2aAgent preserves headers including the proxy host header for proxy routing."""
    with TestHTTPServer() as test_server:
        config = AgentConfig(
            model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
            description="Test agent",
            instruction="You are a test agent",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote_agent",
                    url=test_server.url,  # Use test server as proxy URL
                    description="Remote agent",
                    headers={
                        "Authorization": "Bearer token123",
                        PROXY_HOST_HEADER: "remote-agent.kagent",  # Proxy host header for proxy routing
                    },
                )
            ],
        )

        agent = config.to_agent("test_agent")

        # Find the RemoteA2aAgent tool
        from google.adk.tools.agent_tool import AgentTool

        remote_agent_tool = None
        for tool in agent.tools:
            if isinstance(tool, AgentTool):
                remote_agent_tool = tool.agent
                break

        assert remote_agent_tool is not None

        # Make a request using the client - the client has base_url set to the proxy
        async with remote_agent_tool._httpx_client as client:
            await client.get("/test")

        # Verify headers are preserved in actual requests
        assert len(test_server.requests) > 0
        headers = test_server.requests[0]["headers"]
        assert headers.get("Authorization") == "Bearer token123" or headers.get("authorization") == "Bearer token123"
        assert (
            headers.get(PROXY_HOST_HEADER) == "remote-agent.kagent"
            or headers.get(PROXY_HOST_HEADER.lower()) == "remote-agent.kagent"
        )


@pytest.mark.asyncio
async def test_remote_agent_url_rewrite_event_hook():
    """Test that URL rewrite event hook rewrites URLs to proxy when the proxy host header is present.

    When the proxy host header is present, the event hook rewrites all request URLs to use the proxy
    base URL while preserving the proxy host header. This ensures that even if RemoteA2aAgent
    uses URLs from the agent card response, they still go through the proxy.
    """
    with TestHTTPServer() as test_server:
        config = AgentConfig(
            model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
            description="Test agent",
            instruction="You are a test agent",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote_agent",
                    url=test_server.url,  # Use test server as proxy URL
                    description="Remote agent",
                    headers={PROXY_HOST_HEADER: "remote-agent.kagent"},  # Proxy host header indicates proxy usage
                )
            ],
        )

        agent = config.to_agent("test_agent")

        # Find the RemoteA2aAgent tool
        from google.adk.tools.agent_tool import AgentTool

        remote_agent_tool = None
        for tool in agent.tools:
            if isinstance(tool, AgentTool):
                remote_agent_tool = tool.agent
                break

        assert remote_agent_tool is not None

        # Make a request that would normally use a direct URL
        # The event hook should rewrite it to use the proxy (test server)
        async with remote_agent_tool._httpx_client as client:
            # Simulate what happens when RemoteA2aAgent makes a request using
            # a URL that would normally bypass the proxy (e.g., from agent card response)
            await client.get("http://remote-agent.kagent:8080/some/path")

        # Verify the request was rewritten to use the proxy (test server)
        assert len(test_server.requests) > 0
        # The path should be rewritten to /some/path (proxy base URL + path)
        assert test_server.requests[0]["path"] == "/some/path"
        headers = test_server.requests[0]["headers"]
        assert (
            headers.get(PROXY_HOST_HEADER) == "remote-agent.kagent"
            or headers.get(PROXY_HOST_HEADER.lower()) == "remote-agent.kagent"
        )


def test_mcp_tool_with_proxy_url():
    """Test that MCP tools are configured with proxy URL and the proxy host header.

    When proxy is configured, the URL is set to the proxy URL and the proxy host header
    is included for proxy routing. These are passed through directly to McpToolset.

    Note: We verify connection_params configuration because McpToolset doesn't expose
    a public API to verify proxy setup. The connection_params are what McpToolset uses
    internally to create its HTTP client, so verifying them ensures our configuration
    is correctly applied.
    """
    from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams

    from kagent.adk.types import HttpMcpServerConfig

    # Configuration with proxy URL and proxy host header
    config = AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        http_tools=[
            HttpMcpServerConfig(
                params=StreamableHTTPConnectionParams(
                    url="http://proxy.kagent.svc.cluster.local:8080/mcp",  # Proxy URL
                    headers={PROXY_HOST_HEADER: "test-mcp-server.kagent"},  # Proxy host header for proxy routing
                ),
                tools=["test-tool"],
            )
        ],
    )

    agent = config.to_agent("test_agent")

    # Find the McpToolset
    mcp_tool = None
    for tool in agent.tools:
        if type(tool).__name__ == "McpToolset":
            mcp_tool = tool
            break

    assert mcp_tool is not None, f"No McpToolset found. Tools: {[type(t).__name__ for t in agent.tools]}"

    # Verify connection params are configured correctly
    # Note: We access connection_params (which may be private) because McpToolset doesn't expose
    # a public API to verify connection configuration. We're testing our code's configuration logic.
    connection_params = getattr(mcp_tool, "_connection_params", None) or getattr(mcp_tool, "connection_params", None)
    assert connection_params is not None
    assert connection_params.url == "http://proxy.kagent.svc.cluster.local:8080/mcp"
    assert connection_params.headers is not None
    assert connection_params.headers[PROXY_HOST_HEADER] == "test-mcp-server.kagent"


def test_mcp_tool_without_proxy():
    """Test that MCP tools are configured with direct URL when proxy is not configured.

    Note: We verify connection_params configuration because McpToolset doesn't expose
    a public API to verify connection setup. The connection_params are what McpToolset uses
    internally to create its HTTP client.
    """
    from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams

    from kagent.adk.types import HttpMcpServerConfig

    config = AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        http_tools=[
            HttpMcpServerConfig(
                params=StreamableHTTPConnectionParams(
                    url="http://test-mcp-server.kagent:8084/mcp",  # Direct URL
                    headers=None,  # No headers
                ),
                tools=["test-tool"],
            )
        ],
    )

    agent = config.to_agent("test_agent")

    # Find the McpToolset
    mcp_tool = None
    for tool in agent.tools:
        if type(tool).__name__ == "McpToolset":
            mcp_tool = tool
            break

    assert mcp_tool is not None, f"No McpToolset found. Tools: {[type(t).__name__ for t in agent.tools]}"

    # Verify connection params use the direct URL
    connection_params = getattr(mcp_tool, "_connection_params", None) or getattr(mcp_tool, "connection_params", None)
    assert connection_params is not None
    assert connection_params.url == "http://test-mcp-server.kagent:8084/mcp"


def test_sse_mcp_tool_with_proxy_url():
    """Test that SSE MCP tools are configured with proxy URL and proxy host header.

    When proxy is configured, the URL is set to the proxy URL and the proxy host header
    is included for proxy routing. These are passed through directly to McpToolset.

    Note: We verify connection_params configuration because McpToolset doesn't expose
    a public API to verify proxy setup. The connection_params are what McpToolset uses
    internally to create its HTTP client, so verifying them ensures our configuration
    is correctly applied.
    """
    from google.adk.tools.mcp_tool import SseConnectionParams

    from kagent.adk.types import SseMcpServerConfig

    # Configuration with proxy URL and proxy host header
    config = AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        sse_tools=[
            SseMcpServerConfig(
                params=SseConnectionParams(
                    url="http://proxy.kagent.svc.cluster.local:8080/mcp",  # Proxy URL
                    headers={PROXY_HOST_HEADER: "test-sse-mcp-server.kagent"},  # Proxy host header for proxy routing
                ),
                tools=["test-sse-tool"],
            )
        ],
    )

    agent = config.to_agent("test_agent")

    # Find the McpToolset
    mcp_tool = None
    for tool in agent.tools:
        if type(tool).__name__ == "McpToolset":
            mcp_tool = tool
            break

    assert mcp_tool is not None, f"No McpToolset found. Tools: {[type(t).__name__ for t in agent.tools]}"

    # Verify connection params are configured correctly
    connection_params = getattr(mcp_tool, "_connection_params", None) or getattr(mcp_tool, "connection_params", None)
    assert connection_params is not None
    assert connection_params.url == "http://proxy.kagent.svc.cluster.local:8080/mcp"
    assert connection_params.headers is not None
    assert connection_params.headers[PROXY_HOST_HEADER] == "test-sse-mcp-server.kagent"


def test_sse_mcp_tool_without_proxy():
    """Test that SSE MCP tools are configured with direct URL when proxy is not configured.

    Note: We verify connection_params configuration because McpToolset doesn't expose
    a public API to verify connection setup. The connection_params are what McpToolset uses
    internally to create its HTTP client.
    """
    from google.adk.tools.mcp_tool import SseConnectionParams

    from kagent.adk.types import SseMcpServerConfig

    config = AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        sse_tools=[
            SseMcpServerConfig(
                params=SseConnectionParams(
                    url="http://test-sse-mcp-server.kagent:8084/mcp",  # Direct URL
                    headers=None,  # No headers
                ),
                tools=["test-sse-tool"],
            )
        ],
    )

    agent = config.to_agent("test_agent")

    # Find the McpToolset
    mcp_tool = None
    for tool in agent.tools:
        if type(tool).__name__ == "McpToolset":
            mcp_tool = tool
            break

    assert mcp_tool is not None, f"No McpToolset found. Tools: {[type(t).__name__ for t in agent.tools]}"

    # Verify connection params use the direct URL
    connection_params = getattr(mcp_tool, "_connection_params", None) or getattr(mcp_tool, "connection_params", None)
    assert connection_params is not None
    assert connection_params.url == "http://test-sse-mcp-server.kagent:8084/mcp"
