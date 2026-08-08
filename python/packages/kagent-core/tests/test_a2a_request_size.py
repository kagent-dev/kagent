from collections.abc import AsyncIterator
from unittest.mock import Mock

import pytest
from a2a.server.routes import add_a2a_routes_to_fastapi, create_jsonrpc_routes
from fastapi import FastAPI
from fastapi.testclient import TestClient
from starlette.exceptions import HTTPException
from starlette.types import Message, Scope

from kagent.core.a2a import A2ARequestSizeLimitMiddleware


def test_a2a_dispatcher_returns_jsonrpc_payload_too_large_error():
    app = FastAPI()
    add_a2a_routes_to_fastapi(
        app,
        jsonrpc_routes=create_jsonrpc_routes(Mock(), rpc_url="/"),
    )
    app.add_middleware(A2ARequestSizeLimitMiddleware, max_content_length=5)

    response = TestClient(app).post(
        "/",
        content=b"123456",
        headers={"content-type": "application/json"},
    )

    assert response.status_code == 200
    assert response.json() == {
        "error": {"code": -32600, "message": "Payload too large"},
        "id": None,
        "jsonrpc": "2.0",
    }
    assert "/" in app.openapi()["paths"]


def _scope(*, path: str = "/", content_length: int | None = None) -> Scope:
    headers = []
    if content_length is not None:
        headers.append((b"content-length", str(content_length).encode()))
    return {
        "type": "http",
        "asgi": {"version": "3.0"},
        "http_version": "1.1",
        "method": "POST",
        "scheme": "http",
        "path": path,
        "raw_path": path.encode(),
        "query_string": b"",
        "root_path": "",
        "headers": headers,
        "server": ("testserver", 80),
        "client": ("testclient", 50000),
        "state": {},
    }


def _receive_from(messages: list[Message]):
    iterator: AsyncIterator[Message]

    async def generate() -> AsyncIterator[Message]:
        for message in messages:
            yield message

    iterator = generate()
    return iterator.__anext__


async def _read_body(receive) -> tuple[bytes, int | None]:
    body = bytearray()
    try:
        while True:
            message = await receive()
            body.extend(message.get("body", b""))
            if not message.get("more_body", False):
                return bytes(body), None
    except HTTPException as error:
        return bytes(body), error.status_code


@pytest.mark.asyncio
async def test_rejects_declared_oversized_body_without_reading_it():
    source_reads = 0
    result = None

    async def receive() -> Message:
        nonlocal source_reads
        source_reads += 1
        return {"type": "http.request", "body": b"oversized"}

    async def app(scope, limited_receive, send):
        nonlocal result
        result = await _read_body(limited_receive)

    middleware = A2ARequestSizeLimitMiddleware(app, max_content_length=5)
    await middleware(_scope(content_length=9), receive, lambda message: None)

    assert result == (b"", 413)
    assert source_reads == 0


@pytest.mark.asyncio
async def test_rejects_chunked_body_when_actual_size_exceeds_limit():
    result = None

    async def app(scope, receive, send):
        nonlocal result
        result = await _read_body(receive)

    receive = _receive_from(
        [
            {"type": "http.request", "body": b"abc", "more_body": True},
            {"type": "http.request", "body": b"def", "more_body": False},
        ]
    )
    middleware = A2ARequestSizeLimitMiddleware(app, max_content_length=5)
    await middleware(_scope(), receive, lambda message: None)

    assert result == (b"abc", 413)


@pytest.mark.asyncio
async def test_allows_body_at_exact_limit():
    result = None

    async def app(scope, receive, send):
        nonlocal result
        result = await _read_body(receive)

    receive = _receive_from([{"type": "http.request", "body": b"abcde", "more_body": False}])
    middleware = A2ARequestSizeLimitMiddleware(app, max_content_length=5)
    await middleware(_scope(content_length=5), receive, lambda message: None)

    assert result == (b"abcde", None)


@pytest.mark.asyncio
async def test_unlimited_configuration_bypasses_limit():
    result = None

    async def app(scope, receive, send):
        nonlocal result
        result = await _read_body(receive)

    receive = _receive_from([{"type": "http.request", "body": b"oversized", "more_body": False}])
    middleware = A2ARequestSizeLimitMiddleware(app, max_content_length=None)
    await middleware(_scope(content_length=9), receive, lambda message: None)

    assert result == (b"oversized", None)


@pytest.mark.asyncio
async def test_non_a2a_path_bypasses_limit():
    result = None

    async def app(scope, receive, send):
        nonlocal result
        result = await _read_body(receive)

    receive = _receive_from([{"type": "http.request", "body": b"oversized", "more_body": False}])
    middleware = A2ARequestSizeLimitMiddleware(app, max_content_length=5)
    await middleware(_scope(path="/other", content_length=9), receive, lambda message: None)

    assert result == (b"oversized", None)
