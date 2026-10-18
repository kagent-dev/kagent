import asyncio
import json
from contextlib import asynccontextmanager
from unittest.mock import AsyncMock, MagicMock

import httpx2
import pytest
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams
from kagent.config.v1.adk_pb2 import ADKHTTPConnectionParams, ADKSSEConnectionParams

from kagent.adk._bearer_token import bearer_token
from kagent.adk._mcp_toolset import KAgentMcpToolset
from kagent.adk.types import _mcp_params, create_header_provider


@asynccontextmanager
async def _guard_test_server(protocol, redirect_status=None):
    from aiohttp import web

    received = []
    finished = asyncio.Event()
    stream = None

    async def handle(request):
        nonlocal stream
        received.append((request.path, request.headers.get("Authorization")))
        if request.path == "/redirect-target":
            return web.Response(text="must not be reached", status=500)
        if redirect_status and request.path == "/entry":
            return web.Response(
                status=redirect_status,
                headers={"Location": "/redirect-target?private-location"},
                text="private-response-body",
            )
        if protocol == "sse" and request.method == "GET":
            stream = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
            await stream.prepare(request)
            # A valid SSE endpoint announcement changes path, not origin.
            await stream.write(b"event: endpoint\ndata: /messages\n\n")
            await finished.wait()
            return stream
        message = await request.json()
        if "id" not in message:
            return web.Response(status=202)
        if message["method"] == "initialize":
            result = {
                "protocolVersion": message["params"]["protocolVersion"],
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "guards", "version": "1"},
            }
        else:
            result = {"tools": []}
        response = {"jsonrpc": "2.0", "id": message["id"], "result": result}
        if protocol == "sse":
            await stream.write(f"event: message\ndata: {json.dumps(response)}\n\n".encode())
            return web.Response(status=202)
        return web.json_response(response)

    app = web.Application()
    app.router.add_route("*", "/{path:.*}", handle)
    runner = web.AppRunner(app)
    await runner.setup()
    await web.TCPSite(runner, "127.0.0.1", 0).start()
    try:
        yield f"http://127.0.0.1:{runner.addresses[0][1]}/entry", received
    finally:
        finished.set()
        await runner.cleanup()


@pytest.fixture
def automatic_mtls(monkeypatch):
    from google.adk.tools.mcp_tool import mcp_session_manager as sdk

    monkeypatch.setenv("GOOGLE_API_USE_CLIENT_CERTIFICATE", "true")
    monkeypatch.setattr(sdk, "_AIO_SUPPORTED", True)
    adc = MagicMock(return_value=(MagicMock(), "project"))
    auth_session = MagicMock(is_mtls=True)
    auth_session.configure_mtls_channel = AsyncMock()
    auth_session.close = AsyncMock()
    constructor = MagicMock(return_value=auth_session)
    monkeypatch.setattr(sdk.google.auth, "default", adc)
    monkeypatch.setattr(sdk, "AsyncAuthorizedSession", constructor)
    return adc, constructor


@pytest.mark.parametrize("protocol", ["http", "sse"])
async def test_session_manager_keeps_configured_factory_when_automatic_mtls_available(protocol, automatic_mtls):
    from google.adk.tools.mcp_tool.mcp_session_manager import MCPSessionManager

    config_type, params_type = (
        (ADKHTTPConnectionParams, StreamableHTTPConnectionParams)
        if protocol == "http"
        else (ADKSSEConnectionParams, SseConnectionParams)
    )
    adc, auth_session = automatic_mtls
    async with _guard_test_server(protocol) as (url, received):
        params = _mcp_params(config_type(url=url, tls_insecure_skip_verify=True), params_type, True)
        # Prove the environment would select automatic mTLS for ordinary
        # toolsets; the passthrough-only override must not alter their behavior.
        ordinary = KAgentMcpToolset(connection_params=params)
        assert type(ordinary._mcp_session_manager) is MCPSessionManager
        assert await ordinary._mcp_session_manager._get_mtls_transport() is not None
        adc.assert_called_once()
        auth_session.return_value.configure_mtls_channel.assert_awaited_once()
        await ordinary.close()
        adc.reset_mock()
        auth_session.reset_mock()

        configured_factory = params.httpx_client_factory
        clients = []

        def track_factory(**kwargs):
            client = configured_factory(**kwargs)
            clients.append(client)
            return client

        params.httpx_client_factory = track_factory
        toolset = KAgentMcpToolset(
            connection_params=params,
            header_provider=create_header_provider(bearer_token_passthrough=True),
            bearer_token_passthrough=True,
        )
        marker = bearer_token.set("caller")
        try:
            assert await asyncio.wait_for(toolset.get_tools(), 5) == []
            adc.assert_not_called()
            auth_session.assert_not_called()
            assert len(clients) == 1
            client = clients[0]
            assert not client.follow_redirects
            # The explicitly configured TLS context, not ADC's transport.
            import ssl

            assert client._transport._pool._ssl_context.verify_mode == ssl.CERT_NONE
            with pytest.raises(ValueError, match="cannot change destination"):
                await client.event_hooks["request"][0](httpx2.Request("POST", "https://foreign.example"))
            assert received
            assert all(auth == "Bearer caller" for _, auth in received)
            if protocol == "sse":
                assert any(path == "/messages" for path, _ in received)
        finally:
            await toolset.close()
            bearer_token.reset(marker)


@pytest.mark.parametrize("protocol", ["http", "sse"])
@pytest.mark.parametrize("status", [301, 302, 303, 307, 308])
async def test_session_manager_rejects_redirect_before_sdk_follows(protocol, status, automatic_mtls, caplog):
    config_type, params_type = (
        (ADKHTTPConnectionParams, StreamableHTTPConnectionParams)
        if protocol == "http"
        else (ADKSSEConnectionParams, SseConnectionParams)
    )
    async with _guard_test_server(protocol, status) as (url, received):
        toolset = KAgentMcpToolset(
            connection_params=_mcp_params(config_type(url=url), params_type, True),
            header_provider=create_header_provider(bearer_token_passthrough=True),
            bearer_token_passthrough=True,
        )
        marker = bearer_token.set("caller")
        try:
            with pytest.raises(Exception) as failure:
                await asyncio.wait_for(toolset.get_tools(), 5)
            assert not isinstance(failure.value, TimeoutError)
            assert received
            assert all(path == "/entry" for path, _ in received)
            assert all(auth == "Bearer caller" for _, auth in received)

            def error_messages(error):
                yield str(error)
                if isinstance(error, BaseExceptionGroup):
                    for nested in error.exceptions:
                        yield from error_messages(nested)
                if error.__cause__ is not None:
                    yield from error_messages(error.__cause__)

            messages = "\n".join(error_messages(failure.value))
            assert "does not allow redirects" in messages
            assert url not in messages
            for private in ("private-location", "private-response-body", "/redirect-target"):
                assert private not in messages
                assert private not in caplog.text
            automatic_mtls[0].assert_not_called()
        finally:
            await toolset.close()
            bearer_token.reset(marker)


@pytest.mark.parametrize("enabled", [False, True])
async def test_http_requests_use_current_caller_and_isolate_sessions(enabled):
    from aiohttp import web

    received = []

    async def handle(request):
        if request.method != "POST":
            return web.Response(status=405)
        message = await request.json()
        received.append((message["method"], request.headers.get("Authorization")))
        if "id" not in message:
            return web.Response(status=202)
        if message["method"] == "initialize":
            result = {
                "protocolVersion": message["params"]["protocolVersion"],
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "test", "version": "1"},
            }
        elif message["method"] == "tools/list":
            result = {"tools": [{"name": "echo", "inputSchema": {"type": "object"}}]}
        elif message["method"] == "tools/call":
            result = {"content": [{"type": "text", "text": "ok"}]}
        else:
            result = {}
        return web.json_response({"jsonrpc": "2.0", "id": message["id"], "result": result})

    app = web.Application()
    app.router.add_route("*", "/mcp", handle)
    server = web.AppRunner(app)
    await server.setup()
    site = web.TCPSite(server, "127.0.0.1", 0)
    await site.start()
    params = _mcp_params(
        ADKHTTPConnectionParams(
            url=f"http://127.0.0.1:{server.addresses[0][1]}/mcp",
            headers={"authorization": "static-discovery"},
        ),
        StreamableHTTPConnectionParams,
        enabled,
    )
    toolset = KAgentMcpToolset(
        connection_params=params,
        header_provider=create_header_provider(bearer_token_passthrough=enabled),
        bearer_token_passthrough=enabled,
    )
    marker = bearer_token.set(None)
    try:
        if enabled:
            with pytest.raises(ValueError, match="requires an authenticated invocation"):
                await toolset.get_tools()
            assert not received
        bearer_token.set("alice")
        tools = await toolset.get_tools()
        assert len(tools) == 1
        assert {auth for _, auth in received} == ({"Bearer alice"} if enabled else {"static-discovery"})
        received.clear()
        bearer_token.set("bob")
        # Even a tool discovered by Alice resolves the current token for calls.
        context = MagicMock()
        context.state = {}
        await tools[0].run_async(args={}, tool_context=context)
        assert any(method == "tools/call" for method, _ in received)
        assert {auth for _, auth in received} == ({"Bearer bob"} if enabled else {"static-discovery"})
        assert len(toolset._mcp_session_manager._session_contexts) == (2 if enabled else 1)
        received.clear()
        bearer_token.set(None)
        if enabled:
            result = await tools[0].run_async(args={}, tool_context=context)
            assert "requires an authenticated invocation" in result["error"]
            assert not received
    finally:
        await toolset.close()
        bearer_token.reset(marker)
        await server.cleanup()


async def test_provider_isolated_and_never_falls_back():
    provider = create_header_provider(
        allowed_headers=["Authorization"],
        sts_header_provider=lambda _: {"Authorization": "service-token"},
        bearer_token_passthrough=True,
    )
    marker = bearer_token.set(None)
    try:
        with pytest.raises(ValueError, match="requires an authenticated invocation"):
            provider(None)

        async def invoke(token):
            marker = bearer_token.set(token)
            try:
                await asyncio.sleep(0)
                return provider(None)
            finally:
                bearer_token.reset(marker)

        assert await asyncio.gather(invoke("alice"), invoke("bob")) == [
            {"Authorization": "Bearer alice"},
            {"Authorization": "Bearer bob"},
        ]
        assert bearer_token.get() is None
    finally:
        bearer_token.reset(marker)


@pytest.mark.parametrize(
    "config_type,params_type",
    [(ADKHTTPConnectionParams, StreamableHTTPConnectionParams), (ADKSSEConnectionParams, SseConnectionParams)],
)
async def test_params_filter_static_authorization_and_restrict_destination(config_type, params_type):
    params = _mcp_params(
        config_type(url="https://mcp.example.com/mcp", headers={"aUtHoRiZaTiOn": "discovery", "X-Tenant": "tenant"}),
        params_type,
        True,
    )
    assert params.headers == {"X-Tenant": "tenant"}
    async with params.httpx_client_factory(headers={"Authorization": "Bearer caller"}) as client:
        assert not client.follow_redirects
        hook = client.event_hooks["request"][-1]
        await hook(httpx2.Request("POST", "https://mcp.example.com/alternate"))
        with pytest.raises(ValueError, match="cannot change destination"):
            await hook(httpx2.Request("POST", "https://elsewhere.example.com/mcp"))


async def test_toolset_initialization_missing_token_does_not_dial():
    provider = create_header_provider(bearer_token_passthrough=True)
    toolset = KAgentMcpToolset(
        connection_params=StreamableHTTPConnectionParams(url="https://mcp.example.com"),
        header_provider=provider,
        bearer_token_passthrough=True,
    )
    toolset._mcp_session_manager.create_session = AsyncMock(side_effect=AssertionError("must not dial"))
    # The global propagation plugin must not replace the restricted provider.
    toolset._header_provider = lambda _: {"Authorization": "service-token"}
    marker = bearer_token.set(None)
    try:
        with pytest.raises(ValueError, match="requires an authenticated invocation"):
            await toolset.get_tools()
        toolset._mcp_session_manager.create_session.assert_not_called()
        bearer_token.set("caller")
        assert await toolset._build_headers(None) == {"Authorization": "Bearer caller"}
    finally:
        bearer_token.reset(marker)
        await toolset.close()


def test_disabled_passthrough_does_not_read_context_token():
    marker = bearer_token.set("caller")
    try:
        assert create_header_provider() is None
    finally:
        bearer_token.reset(marker)


async def test_request_headers_never_enter_native_session_database(tmp_path):
    from google.adk.agents import BaseAgent
    from google.adk.events import Event
    from google.adk.runners import Runner
    from google.genai import types

    from kagent.adk._local_session_service import LocalSessionService

    token = "passthrough-test-credential-not-for-persistence"
    seen = []

    class InspectHeaders(BaseAgent):
        async def _run_async_impl(self, ctx):
            seen.append(ctx.session.state.get("temp:headers"))
            yield Event(author=self.name, content=types.Content(role="model", parts=[types.Part(text="ok")]))

    db = tmp_path / "session.db"
    service = LocalSessionService(db_url=f"sqlite+aiosqlite:///{db}")
    try:
        await service.create_session(app_name="test", user_id="user", session_id="session")
        runner = Runner(app_name="test", agent=InspectHeaders(name="inspect"), session_service=service)
        async for _ in runner.run_async(
            user_id="user",
            session_id="session",
            new_message=types.Content(role="user", parts=[types.Part(text="hello")]),
            state_delta={"temp:headers": {"Authorization": f"Bearer {token}"}},
        ):
            pass
        stored = await service.get_session(app_name="test", user_id="user", session_id="session")
        assert "temp:headers" not in stored.state
        assert "headers" not in stored.state
        assert token not in str(stored.events)
    finally:
        await service.close()
    assert seen == [{"Authorization": f"Bearer {token}"}]
    assert token.encode() not in db.read_bytes()
