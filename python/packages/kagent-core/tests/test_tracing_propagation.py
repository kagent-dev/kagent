"""Tests for the outbound trace-context injection httpx hook (memory hop)."""

import httpx
import pytest
from opentelemetry.sdk.trace import TracerProvider

from kagent.core.tracing import inject_trace_context


def _make_request() -> httpx.Request:
    return httpx.Request("POST", "http://kagent-controller/api/memories/search")


@pytest.mark.asyncio
async def test_inject_adds_traceparent_when_span_active():
    # A real (non-recording) provider is enough: an active span yields a valid
    # SpanContext that the W3C propagator serializes into `traceparent`.
    provider = TracerProvider()
    tracer = provider.get_tracer("test")

    request = _make_request()
    with tracer.start_as_current_span("memory.read"):
        await inject_trace_context(request)

    assert "traceparent" in request.headers
    # W3C traceparent format: version-traceid-spanid-flags (3 hyphens).
    assert request.headers["traceparent"].count("-") == 3


@pytest.mark.asyncio
async def test_inject_is_noop_without_active_span():
    # No active/recording span -> empty carrier -> request headers untouched.
    request = _make_request()
    await inject_trace_context(request)
    assert "traceparent" not in request.headers


@pytest.mark.asyncio
async def test_inject_preserves_existing_headers():
    provider = TracerProvider()
    tracer = provider.get_tracer("test")

    request = _make_request()
    request.headers["Authorization"] = "Bearer abc"
    with tracer.start_as_current_span("memory.read"):
        await inject_trace_context(request)

    assert request.headers["Authorization"] == "Bearer abc"
    assert "traceparent" in request.headers
