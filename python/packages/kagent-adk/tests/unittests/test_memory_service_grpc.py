from unittest.mock import AsyncMock, MagicMock

import pytest
from google.adk.events import Event
from google.adk.sessions import Session
from google.genai import types
from kagent.api.v1alpha1 import memory_pb2

from kagent.adk._memory_service import KagentMemoryService


@pytest.fixture
def client():
    value = MagicMock()
    value.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
    value.memory_service = MagicMock()
    value.memory_service.AddSession = AsyncMock(return_value=memory_pb2.MemoryServiceAddSessionResponse(id="memory-1"))
    value.memory_service.AddSessionBatch = AsyncMock(
        return_value=memory_pb2.MemoryServiceAddSessionBatchResponse(count=2)
    )
    value.memory_service.Search = AsyncMock(
        return_value=memory_pb2.MemoryServiceSearchResponse(
            memories=[memory_pb2.MemorySearchResult(id="memory-2", content="remember this", score=0.9)]
        )
    )
    return value


@pytest.fixture
def service(client):
    value = KagentMemoryService(agent_name="ns__NS__agent", controller_client=client, ttl_days=7)
    value._embedding_client = MagicMock()
    return value


@pytest.mark.asyncio
async def test_add_memory_uses_generated_rpc_with_metadata_and_ttl(service, client):
    service._embedding_client.generate = AsyncMock(return_value=[0.25, 0.75])

    await service.add_memory(
        app_name="ignored",
        user_id="user-1",
        content="remember this",
        metadata={"session_id": "session-1", "source": "explicit_save"},
    )

    request = client.memory_service.AddSession.await_args.args[0]
    assert request.memory.agent_name == "ns__NS__agent"
    assert request.memory.user_id == "user-1"
    assert request.memory.content == "remember this"
    assert list(request.memory.vector) == [0.25, 0.75]
    assert request.memory.ttl_days == 7
    assert request.memory.metadata["session_id"] == "session-1"
    client.call_options.assert_awaited_once_with("user-1")


@pytest.mark.asyncio
async def test_search_memory_uses_defaults_and_maps_results(service, client):
    service._embedding_client.generate = AsyncMock(return_value=[0.5, 0.125])

    response = await service.search_memory(app_name="ignored", user_id="user-2", query="what matters?")

    request = client.memory_service.Search.await_args.args[0]
    assert request.agent_name == "ns__NS__agent"
    assert request.user_id == "user-2"
    assert list(request.vector) == [0.5, 0.125]
    assert request.limit == 5
    assert request.min_score == pytest.approx(0.3)
    assert len(response.memories) == 1
    assert response.memories[0].id == "memory-2"
    assert response.memories[0].content.parts[0].text == "remember this"
    client.call_options.assert_awaited_once_with("user-2")


@pytest.mark.asyncio
async def test_session_memory_batches_generated_inputs(service, client):
    service._summarize_session_content_async = AsyncMock(return_value=["fact one", "fact two"])
    service._embedding_client.generate = AsyncMock(return_value=[[0.1, 0.2], [0.3, 0.4]])
    session = Session(
        id="session-1",
        app_name="agent",
        user_id="user-3",
        events=[
            Event(
                author="user",
                invocation_id="invocation-1",
                content=types.Content(role="user", parts=[types.Part(text="hello")]),
            )
        ],
    )

    await service._add_session_to_memory_background(session)

    request = client.memory_service.AddSessionBatch.await_args.args[0]
    assert [item.content for item in request.items] == ["fact one", "fact two"]
    assert [list(item.vector) for item in request.items] == [
        pytest.approx([0.1, 0.2]),
        pytest.approx([0.3, 0.4]),
    ]
    assert all(item.ttl_days == 7 for item in request.items)
    client.call_options.assert_awaited_once_with("user-3")
