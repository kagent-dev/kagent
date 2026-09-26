import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest
from google.adk.events import Event
from google.adk.sessions import Session
from google.genai import types
from kagent.api.v1alpha1 import memory_pb2

from kagent.adk._memory_service import KagentMemoryService
from kagent.adk._request_identity import request_user_id


@pytest.fixture
def caller():
    token = request_user_id.set("alice")
    yield "alice"
    request_user_id.reset(token)


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
async def test_add_memory_uses_generated_rpc_with_metadata_and_ttl(service, client, caller):
    service._embedding_client.generate = AsyncMock(return_value=[0.25, 0.75])

    await service.add_memory(
        app_name="ignored",
        content="remember this",
        metadata={"session_id": "session-1", "source": "explicit_save"},
    )

    request = client.memory_service.AddSession.await_args.args[0]
    assert request.memory.agent_name == "ns__NS__agent"
    assert request.memory.user_id == caller
    assert request.memory.content == "remember this"
    assert list(request.memory.vector) == [0.25, 0.75]
    assert request.memory.ttl_days == 7
    assert request.memory.metadata["session_id"] == "session-1"
    client.call_options.assert_awaited_once_with(caller)


@pytest.mark.asyncio
async def test_search_memory_uses_defaults_and_maps_results(service, client, caller):
    service._embedding_client.generate = AsyncMock(return_value=[0.5, 0.125])

    response = await service.search_memory(app_name="ignored", user_id="user-2", query="what matters?")

    request = client.memory_service.Search.await_args.args[0]
    assert request.agent_name == "ns__NS__agent"
    assert request.user_id == caller
    assert list(request.vector) == [0.5, 0.125]
    assert request.limit == 5
    assert request.min_score == pytest.approx(0.3)
    assert len(response.memories) == 1
    assert response.memories[0].id == "memory-2"
    assert response.memories[0].content.parts[0].text == "remember this"
    client.call_options.assert_awaited_once_with(caller)


@pytest.mark.asyncio
async def test_session_memory_batches_generated_inputs(service, client, caller):
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
    assert all(item.user_id == caller for item in request.items)
    client.call_options.assert_awaited_once_with(caller)


async def test_memory_uses_request_user_instead_of_private_native_key(service, client):
    service._embedding_client.generate = AsyncMock(return_value=[0.5, 0.125])
    token = request_user_id.set("alice")
    try:
        await service.add_memory(app_name="app", content="remember")
        await service.search_memory(app_name="app", user_id="conversation", query="remember")
    finally:
        request_user_id.reset(token)
    assert client.memory_service.AddSession.await_args.args[0].memory.user_id == "alice"
    assert client.memory_service.Search.await_args.args[0].user_id == "alice"
    assert all(call.args == ("alice",) for call in client.call_options.await_args_list)


@pytest.mark.parametrize("operation", ["add", "search", "session", "background session"])
async def test_memory_requires_caller_before_external_work(service, client, operation):
    service._embedding_client.generate = AsyncMock()
    service._summarize_session_content_async = AsyncMock()
    session = Session(id="conversation", app_name="agent", user_id="conversation")
    token = request_user_id.set("")
    try:
        with pytest.raises(ValueError, match="memory requires caller identity"):
            if operation == "add":
                await service.add_memory(app_name="agent", content="remember")
            elif operation == "search":
                await service.search_memory(app_name="agent", user_id="conversation", query="remember")
            elif operation == "session":
                await service.add_session_to_memory(session)
            else:
                await service._add_session_to_memory_background(session)
    finally:
        request_user_id.reset(token)
    service._embedding_client.generate.assert_not_awaited()
    service._summarize_session_content_async.assert_not_awaited()
    client.call_options.assert_not_awaited()
    client.memory_service.AddSession.assert_not_awaited()
    client.memory_service.AddSessionBatch.assert_not_awaited()
    client.memory_service.Search.assert_not_awaited()


async def test_background_memory_keeps_scheduling_callers_identity(service, client, monkeypatch):
    service._summarize_session_content_async = AsyncMock(return_value=["remember"])
    service._embedding_client.generate = AsyncMock(return_value=[[0.1, 0.2]])
    session = Session(
        id="conversation",
        app_name="agent",
        user_id="conversation",
        events=[Event(author="user", content=types.Content(parts=[types.Part(text="remember")]))],
    )
    # Capture the real scheduled task so teardown waits for its work to finish.
    tasks = []
    create_task = asyncio.create_task

    def capture_task(coro):
        task = create_task(coro)
        tasks.append(task)
        return task

    monkeypatch.setattr("kagent.adk._memory_service.asyncio.create_task", capture_task)
    token = request_user_id.set("alice")
    try:
        await service.add_session_to_memory(session)
        request_user_id.set("bob")
        await asyncio.gather(*tasks)
    finally:
        request_user_id.reset(token)
    request = client.memory_service.AddSessionBatch.await_args.args[0]
    assert request.items[0].user_id == "alice"
    client.call_options.assert_awaited_once_with("alice")
