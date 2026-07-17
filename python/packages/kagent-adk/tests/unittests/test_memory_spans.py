"""Tests for kagent memory.* OTel span instrumentation (parity with Go ADK)."""

from unittest.mock import AsyncMock, MagicMock

import pytest
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from kagent.adk import _memory_telemetry as mtel
from kagent.adk._memory_service import KagentMemoryService


@pytest.fixture
def exporter(monkeypatch):
    """Route memory spans into an in-memory exporter for assertions."""
    exp = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exp))
    # _TRACER is captured at import; patch it to our isolated provider.
    monkeypatch.setattr(mtel, "_TRACER", provider.get_tracer("test"))
    return exp


def _span_by_name(exporter, name):
    for s in exporter.get_finished_spans():
        if s.name == name:
            return s
    return None


def _make_service():
    svc = KagentMemoryService(agent_name="test-agent", http_client=MagicMock(), embedding_config=None, ttl_days=15)
    svc._embedding_client = MagicMock()
    svc._embedding_client.generate = AsyncMock(return_value=[0.1, 0.2, 0.3])
    return svc


def _mock_response(json_value):
    resp = MagicMock()
    resp.status_code = 200
    resp.raise_for_status = MagicMock()
    resp.json = MagicMock(return_value=json_value)
    return resp


@pytest.mark.asyncio
async def test_add_memory_emits_write_span(exporter):
    svc = _make_service()
    svc.client.post = AsyncMock(return_value=_mock_response({"id": "m1"}))

    await svc.add_memory(app_name="app", user_id="u1", content="the secret code is BLUE-PANGOLIN-42")

    span = _span_by_name(exporter, mtel.SPAN_MEMORY_WRITE)
    assert span is not None, "expected a memory.write span from the add_memory (save_memory) path"
    attrs = span.attributes
    assert attrs[mtel.ATTR_MEMORY_OPERATION] == mtel.MEMORY_OPERATION_SAVE
    # Explicit saves store content verbatim -> source=user, no summarization.
    assert attrs[mtel.ATTR_MEMORY_SOURCE] == mtel.MEMORY_SOURCE_USER
    assert attrs[mtel.ATTR_MEMORY_SCOPE] == mtel.MEMORY_SCOPE_USER
    assert attrs[mtel.ATTR_MEMORY_INDEX_REF] == "test-agent"
    assert attrs[mtel.ATTR_MEMORY_SUT_STORE_BACKEND] == "pgvector"
    assert attrs[mtel.ATTR_MEMORY_ITEM_COUNT] == 1

    # The verbatim save path must NOT emit a consolidate span.
    assert _span_by_name(exporter, mtel.SPAN_MEMORY_CONSOLIDATE) is None


@pytest.mark.asyncio
async def test_search_memory_emits_read_span_injected(exporter):
    svc = _make_service()
    svc.client.post = AsyncMock(return_value=_mock_response([{"id": "m1", "content": "fact"}]))

    resp = await svc.search_memory(app_name="app", user_id="u1", query="q")
    assert len(resp.memories) == 1

    span = _span_by_name(exporter, mtel.SPAN_MEMORY_READ)
    assert span is not None, "expected a memory.read span"
    attrs = span.attributes
    assert attrs[mtel.ATTR_MEMORY_OPERATION] == mtel.MEMORY_OPERATION_PREFETCH
    assert attrs[mtel.ATTR_MEMORY_INJECTION_RESULT] == mtel.MEMORY_INJECTION_INJECTED
    assert attrs[mtel.ATTR_MEMORY_ITEM_COUNT] == 1
    assert attrs[mtel.ATTR_MEMORY_SUT_NAME] == "kagent"


@pytest.mark.asyncio
async def test_search_memory_emits_read_span_filtered(exporter):
    svc = _make_service()
    svc.client.post = AsyncMock(return_value=_mock_response([]))

    resp = await svc.search_memory(app_name="app", user_id="u1", query="q")
    assert len(resp.memories) == 0

    span = _span_by_name(exporter, mtel.SPAN_MEMORY_READ)
    assert span is not None
    assert span.attributes[mtel.ATTR_MEMORY_INJECTION_RESULT] == mtel.MEMORY_INJECTION_FILTERED
    assert span.attributes[mtel.ATTR_MEMORY_ITEM_COUNT] == 0


@pytest.mark.asyncio
async def test_search_memory_emits_embed_child_and_query_attrs(exporter):
    svc = _make_service()
    svc.client.post = AsyncMock(return_value=_mock_response([{"id": "m1", "content": "fact"}]))

    await svc.search_memory(app_name="app", user_id="u1", query="q")

    # The query-vectorization step is now an explicit child of memory.read.
    embed = _span_by_name(exporter, mtel.SPAN_MEMORY_EMBED)
    assert embed is not None, "expected a memory.embed child span from the recall path"
    assert embed.attributes[mtel.ATTR_MEMORY_OPERATION] == mtel.MEMORY_OPERATION_EMBED
    assert embed.attributes[mtel.ATTR_MEMORY_ITEM_COUNT] == 1

    # The read span carries the actual pgvector query shape.
    read = _span_by_name(exporter, mtel.SPAN_MEMORY_READ)
    assert read.attributes[mtel.ATTR_MEMORY_QUERY_TOP_K] == 5
    assert read.attributes[mtel.ATTR_MEMORY_QUERY_MIN_SCORE] == pytest.approx(0.3)


@pytest.mark.asyncio
async def test_add_memory_emits_embed_child(exporter):
    svc = _make_service()
    svc.client.post = AsyncMock(return_value=_mock_response({"id": "m1"}))

    await svc.add_memory(app_name="app", user_id="u1", content="remember this")

    embed = _span_by_name(exporter, mtel.SPAN_MEMORY_EMBED)
    assert embed is not None, "expected a memory.embed child span from the save path"
    assert embed.attributes[mtel.ATTR_MEMORY_ITEM_COUNT] == 1
