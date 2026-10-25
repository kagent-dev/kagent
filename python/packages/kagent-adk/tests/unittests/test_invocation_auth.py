import asyncio

import httpx
import pytest

from kagent.adk._bearer_token import bearer_token
from kagent.adk._invocation_auth import InvocationCredentialError, authentication_hook, policy_url, request_headers


@pytest.fixture(autouse=True)
def clear_credentials():
    source = request_headers.set({})
    policy = policy_url.set(None)
    token = bearer_token.set(None)
    yield
    bearer_token.reset(token)
    request_headers.reset(source)
    policy_url.reset(policy)


async def test_mcp_background_requests_keep_the_opening_invocation():
    bindings = [{"name": "Authorization", "prefix": "Bearer "}, {"name": "X-Token", "prefix": ""}]
    bearer_token.set("machine")
    hook = authentication_hook(bindings, snapshot=True)
    bearer_token.set("later-human")
    request = httpx.Request("POST", "https://mcp.example.com/mcp")
    await asyncio.create_task(hook(request))
    assert request.headers["Authorization"] == "Bearer machine"
    assert request.headers["X-Token"] == "machine"


async def test_model_calls_use_the_current_invocation_and_fail_without_credentials():
    hook = authentication_hook([{"name": "Authorization", "prefix": "Bearer "}])
    for caller in ("human", "machine"):
        bearer_token.set(caller)
        request = httpx.Request("POST", "https://model.example.com/chat")
        await hook(request)
        assert request.headers["Authorization"] == f"Bearer {caller}"
    bearer_token.set(None)
    with pytest.raises(InvocationCredentialError):
        await hook(httpx.Request("POST", "https://model.example.com/chat"))


async def test_live_policy_uses_fixed_invocation_token_and_never_falls_back(monkeypatch):
    current = {"status": 200, "header": "X-API-Key"}
    calls = []

    def handle(request):
        calls.append(request)
        assert request.url == "http://controller/runtime/egress"
        assert request.headers["Authorization"] == "Bearer kagent-credential-injected"
        return httpx.Response(
            current["status"],
            json=[{"origin": "https://mcp.example.com:443", "passthrough": [{"name": current["header"]}]}],
        )

    original_client = httpx.AsyncClient
    monkeypatch.setattr(
        httpx, "AsyncClient", lambda **kwargs: original_client(transport=httpx.MockTransport(handle), **kwargs)
    )
    policy_url.set("http://controller/runtime/egress")
    bearer_token.set("machine")
    hook = authentication_hook([{"name": "X-API-Key"}], snapshot=True)
    bearer_token.set("later-human")
    for header in ("X-API-Key", "X-Caller"):
        current["header"] = header
        request = httpx.Request("POST", "https://mcp.example.com/mcp")
        await hook(request)
        assert request.headers[header] == "machine"
    assert len(calls) == 2
    current["status"] = 401
    with pytest.raises(InvocationCredentialError):
        await hook(httpx.Request("POST", "https://mcp.example.com/mcp"))


@pytest.mark.parametrize(
    "endpoint,allowed",
    [
        ("https://MCP.EXAMPLE.COM/mcp", True),
        ("https://mcp.example.com./mcp", True),
        ("https://mcp.example.com:0443/mcp", True),
        ("http://mcp.example.com:443/mcp", False),
        ("https://mcp.example.com:8443/mcp", False),
        ("https://other.example.com/mcp", False),
    ],
)
async def test_live_policy_canonical_origin(monkeypatch, endpoint, allowed):
    async def targets(_):
        return [{"origin": "https://mcp.example.com:443", "passthrough": [{"name": "X-Caller"}]}]

    monkeypatch.setattr("kagent.adk._invocation_auth._targets", targets)
    policy_url.set("http://controller/runtime/egress")
    bearer_token.set("caller")
    hook = authentication_hook([{"name": "X-Caller"}])
    request = httpx.Request("POST", endpoint)
    if allowed:
        await hook(request)
        assert request.headers["X-Caller"] == "caller"
    else:
        with pytest.raises(InvocationCredentialError):
            await hook(request)
        assert "X-Caller" not in request.headers
