"""Tests for CrewAI persistence over the generated gRPC client."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, call

import grpc
import pytest
from kagent.api.v1alpha1 import crewai_pb2
from kagent.core import decode_structured_object, encode_structured_object

from kagent.crewai._memory import KagentMemoryStorage
from kagent.crewai._state import KagentFlowPersistence

MAX_MESSAGE_BYTES = 16 << 20
API_VERSION = "kagent.api/v1alpha1"


@pytest.fixture
def client():
    value = MagicMock()
    value.max_message_bytes = MAX_MESSAGE_BYTES
    value.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
    value.crewai_service = MagicMock()
    value.crewai_service.StoreMemory = AsyncMock(return_value=crewai_pb2.StoreMemoryResponse())
    value.crewai_service.GetMemory = AsyncMock(return_value=crewai_pb2.GetMemoryResponse())
    value.crewai_service.ResetMemory = AsyncMock(return_value=crewai_pb2.ResetMemoryResponse())
    value.crewai_service.StoreFlowState = AsyncMock(return_value=crewai_pb2.StoreFlowStateResponse())
    value.crewai_service.GetFlowState = AsyncMock()
    return value


def _rpc_error(code: grpc.StatusCode) -> grpc.aio.AioRpcError:
    return grpc.aio.AioRpcError(code, (), (), "rpc failed", "")


async def test_memory_storage_marshals_worker_thread_calls_to_controller_loop(client):
    memory_data = {
        "task_description": "research grpc",
        "score": 0.75,
        "metadata": {"source": "test"},
        "datetime": "2026-01-01T00:00:00Z",
    }
    client.crewai_service.GetMemory.return_value = crewai_pb2.GetMemoryResponse(
        memories=[
            crewai_pb2.CrewAIMemory(
                thread_id="thread-1",
                user_id="user-1",
                memory_data=encode_structured_object(
                    memory_data,
                    api_version=API_VERSION,
                    kind="CrewAIMemoryData",
                    max_bytes=MAX_MESSAGE_BYTES,
                ),
            )
        ]
    )
    storage = KagentMemoryStorage(
        thread_id="thread-1",
        user_id="user-1",
        client=client,
        loop=asyncio.get_running_loop(),
    )

    await asyncio.to_thread(
        storage.save,
        memory_data["task_description"],
        memory_data["metadata"],
        memory_data["datetime"],
        memory_data["score"],
    )
    loaded = await asyncio.to_thread(storage.load, "grpc", 3)
    await asyncio.to_thread(storage.reset)

    store_request = client.crewai_service.StoreMemory.await_args.args[0]
    assert store_request.thread_id == "thread-1"
    assert (
        decode_structured_object(
            store_request.memory_data,
            expected_kind="CrewAIMemoryData",
            max_bytes=MAX_MESSAGE_BYTES,
        )
        == memory_data
    )
    get_request = client.crewai_service.GetMemory.await_args.args[0]
    assert get_request.task_description == "grpc"
    assert get_request.limit == 3
    assert loaded == [
        {
            "metadata": {"source": "test"},
            "datetime": "2026-01-01T00:00:00Z",
            "score": 0.75,
        }
    ]
    reset_request = client.crewai_service.ResetMemory.await_args.args[0]
    assert reset_request.thread_id == "thread-1"
    assert client.call_options.await_args_list == [call("user-1"), call("user-1"), call("user-1")]


async def test_memory_storage_rejects_sync_call_on_controller_loop(client):
    storage = KagentMemoryStorage(
        thread_id="thread-1",
        user_id="user-1",
        client=client,
        loop=asyncio.get_running_loop(),
    )

    with pytest.raises(RuntimeError, match="cannot run on the controller event loop"):
        storage.reset()


async def test_flow_persistence_prefetches_and_flushes_ordered_structured_writes(client):
    client.crewai_service.GetFlowState.return_value = crewai_pb2.GetFlowStateResponse(
        state=crewai_pb2.CrewAIFlowState(
            thread_id="thread-1",
            method_name="previous",
            state_data=encode_structured_object(
                {"step": 1},
                api_version=API_VERSION,
                kind="CrewAIFlowStateData",
                max_bytes=MAX_MESSAGE_BYTES,
            ),
        )
    )

    persistence = await KagentFlowPersistence.create("thread-1", "user-1", client)
    persistence.save_state("flow-1", "first", {"step": 2})
    persistence.save_state("flow-1", "second", {"step": 3})
    await persistence.flush()

    get_request = client.crewai_service.GetFlowState.await_args.args[0]
    assert get_request.thread_id == "thread-1"
    assert persistence.load_state("flow-1") == {"step": 3}
    requests = [args.args[0] for args in client.crewai_service.StoreFlowState.await_args_list]
    assert [request.method_name for request in requests] == ["first", "second"]
    assert [
        decode_structured_object(
            request.state_data,
            expected_kind="CrewAIFlowStateData",
            max_bytes=MAX_MESSAGE_BYTES,
        )
        for request in requests
    ] == [{"step": 2}, {"step": 3}]
    assert client.call_options.await_args_list == [call("user-1"), call("user-1"), call("user-1")]


async def test_flow_persistence_maps_not_found_to_empty_state(client):
    client.crewai_service.GetFlowState.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND)

    persistence = await KagentFlowPersistence.create("thread-1", "user-1", client)

    assert persistence.load_state("flow-1") is None


async def test_flow_persistence_propagates_other_rpc_errors(client):
    error = _rpc_error(grpc.StatusCode.PERMISSION_DENIED)
    client.crewai_service.GetFlowState.side_effect = error

    with pytest.raises(grpc.aio.AioRpcError) as caught:
        await KagentFlowPersistence.create("thread-1", "user-1", client)

    assert caught.value is error
