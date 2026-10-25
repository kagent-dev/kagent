import asyncio
import json

import httpx
import pytest

from kagent.adk._bearer_token import bearer_token
from kagent.adk._invocation_auth import (
    HEADERS_METADATA_KEY,
    InvocationCredentialError,
    authentication_hook,
    invocation_targets,
    parse_targets,
    request_headers,
)


@pytest.fixture(autouse=True)
def clear_credentials():
    source = request_headers.set({})
    policy = invocation_targets.set(None)
    token = bearer_token.set(None)
    yield
    bearer_token.reset(token)
    request_headers.reset(source)
    invocation_targets.reset(policy)


async def test_mcp_background_requests_keep_the_opening_invocation():
    bindings = [{"name": "Authorization", "prefix": "Bearer "}, {"name": "X-Token", "prefix": ""}]
    bearer_token.set("machine")
    invocation_targets.set(
        parse_targets(
            {HEADERS_METADATA_KEY: json.dumps([{"origin": "https://mcp.example.com:443", "passthrough": bindings}])}
        )
    )
    hook = authentication_hook(snapshot=True)
    bearer_token.set("later-human")
    request = httpx.Request("POST", "https://mcp.example.com/mcp")
    await asyncio.create_task(hook(request))
    assert request.headers["Authorization"] == "Bearer machine"
    assert request.headers["X-Token"] == "machine"


async def test_model_calls_use_the_current_invocation_and_fail_without_credentials():
    invocation_targets.set(
        parse_targets(
            {
                HEADERS_METADATA_KEY: json.dumps(
                    [
                        {
                            "origin": "https://model.example.com:443",
                            "passthrough": [{"name": "Authorization", "prefix": "Bearer "}],
                        }
                    ]
                )
            }
        )
    )
    hook = authentication_hook()
    for caller in ("human", "machine"):
        bearer_token.set(caller)
        request = httpx.Request("POST", "https://model.example.com/chat")
        await hook(request)
        assert request.headers["Authorization"] == f"Bearer {caller}"
    bearer_token.set(None)
    with pytest.raises(InvocationCredentialError):
        await hook(httpx.Request("POST", "https://model.example.com/chat"))


async def test_invocation_snapshot_keeps_its_token_and_bindings(monkeypatch):
    def no_control_plane_calls(**kwargs):
        pytest.fail("header application must not open an HTTP client")

    monkeypatch.setattr(httpx, "AsyncClient", no_control_plane_calls)
    invocation_targets.set(
        parse_targets(
            {
                HEADERS_METADATA_KEY: json.dumps(
                    [{"origin": "https://mcp.example.com:443", "passthrough": [{"name": "X-Caller"}]}]
                ).encode()
            }
        )
    )
    bearer_token.set("machine")
    hook = authentication_hook(snapshot=True)
    invocation_targets.set([])
    bearer_token.set("later-human")
    for _ in range(3):
        request = httpx.Request("POST", "https://mcp.example.com/mcp")
        await asyncio.create_task(hook(request))
        assert request.headers["X-Caller"] == "machine"
    next_invocation = authentication_hook()
    request = httpx.Request("POST", "https://mcp.example.com/mcp")
    await next_invocation(request)
    assert "X-Caller" not in request.headers


@pytest.mark.parametrize("value", [b"null", b"{}", b"invalid", b"[{}]", b'[{"origin": 1}]'])
def test_malformed_snapshot_is_rejected(value):
    with pytest.raises(InvocationCredentialError, match="Invalid invocation header bindings"):
        parse_targets({HEADERS_METADATA_KEY: value})


async def test_policy_only_headers_use_the_invocation_snapshot():
    invocation_targets.set(
        parse_targets(
            {
                HEADERS_METADATA_KEY: json.dumps(
                    [
                        {
                            "origin": "https://model.example.com:443",
                            "headers": {"X-Key": "kagent-credential-injected"},
                            "passthrough": [{"name": "X-Caller"}],
                        }
                    ]
                )
            }
        )
    )
    bearer_token.set("caller")
    request = httpx.Request("POST", "https://model.example.com/chat")
    await authentication_hook()(request)
    assert request.headers["X-Key"] == "kagent-credential-injected"
    assert request.headers["X-Caller"] == "caller"


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
async def test_snapshot_canonical_origin(endpoint, allowed):
    invocation_targets.set(
        parse_targets(
            {
                HEADERS_METADATA_KEY: json.dumps(
                    [{"origin": "https://mcp.example.com:443", "passthrough": [{"name": "X-Caller"}]}]
                )
            }
        )
    )
    bearer_token.set("caller")
    hook = authentication_hook()
    request = httpx.Request("POST", endpoint)
    if allowed:
        await hook(request)
        assert request.headers["X-Caller"] == "caller"
    else:
        await hook(request)
        assert "X-Caller" not in request.headers
