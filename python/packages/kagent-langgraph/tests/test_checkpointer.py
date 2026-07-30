"""Tests for the generated-gRPC LangGraph checkpointer."""

import asyncio
import json
from unittest.mock import AsyncMock, MagicMock, patch

import grpc
import pytest
from kagent.api.v1alpha1 import langgraph_pb2
from langgraph.checkpoint.serde.base import SerializerProtocol

from kagent.langgraph._checkpointer import KAgentCheckpointer


class FakeSerde(SerializerProtocol):
    """A fake serializer that satisfies the SerializerProtocol runtime check."""

    def __init__(self) -> None:
        self.loads: list[tuple[str, bytes]] = []

    def dumps_typed(self, obj):
        return ("json", b'\x00{"fake": true}\xff')

    def loads_typed(self, data):
        self.loads.append(data)
        return {"decoded": data[1]}


@pytest.fixture
def mock_serde():
    return FakeSerde()


@pytest.fixture
def config():
    return {
        "configurable": {
            "thread_id": "test-thread",
            "checkpoint_ns": "",
            "checkpoint_id": "chk-parent",
            "user_id": "admin@kagent.dev",
        }
    }


@pytest.fixture
def checkpoint():
    return {"id": "chk-1", "v": 1, "ts": "2024-01-01T00:00:00Z"}


@pytest.fixture
def metadata():
    return {"source": "test"}


@pytest.fixture
def client():
    value = MagicMock()
    value.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
    value.langgraph_service = MagicMock()
    value.langgraph_service.PutCheckpoint = AsyncMock(return_value=langgraph_pb2.PutCheckpointResponse())
    value.langgraph_service.PutWrites = AsyncMock(return_value=langgraph_pb2.PutWritesResponse())
    value.langgraph_service.ListCheckpoints = AsyncMock(return_value=langgraph_pb2.ListCheckpointsResponse())
    return value


def _rpc_error(code: grpc.StatusCode, details: str = "rpc failed") -> grpc.aio.AioRpcError:
    return grpc.aio.AioRpcError(code, (), (), details, "")


class TestAputRetry:
    """Tests for aput retry logic."""

    async def test_aput_sends_generated_request_and_raw_bytes(self, client, mock_serde, config, checkpoint, metadata):
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        result = await checkpointer.aput(config, checkpoint, metadata, {})

        assert result["configurable"]["checkpoint_id"] == "chk-1"
        request = client.langgraph_service.PutCheckpoint.await_args.args[0]
        assert isinstance(request, langgraph_pb2.PutCheckpointRequest)
        assert request.checkpoint.thread_id == "test-thread"
        assert request.checkpoint.checkpoint_id == "chk-1"
        assert request.checkpoint.parent_checkpoint_id == "chk-parent"
        assert request.checkpoint.checkpoint == b'\x00{"fake": true}\xff'
        assert json.loads(request.checkpoint.metadata) == {
            "source": "test",
            "user_id": "admin@kagent.dev",
        }
        assert request.checkpoint.type == "json"
        client.call_options.assert_awaited_once_with("admin@kagent.dev")

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_retries_transient_rpc_error(self, mock_sleep, client, mock_serde, config, checkpoint, metadata):
        client.langgraph_service.PutCheckpoint.side_effect = [
            _rpc_error(grpc.StatusCode.UNAVAILABLE),
            _rpc_error(grpc.StatusCode.DEADLINE_EXCEEDED),
            langgraph_pb2.PutCheckpointResponse(),
        ]
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        await checkpointer.aput(config, checkpoint, metadata, {})

        assert client.langgraph_service.PutCheckpoint.await_count == 3
        assert mock_sleep.await_count == 2

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_raises_after_transient_retries_exhausted(
        self, mock_sleep, client, mock_serde, config, checkpoint, metadata
    ):
        error = _rpc_error(grpc.StatusCode.RESOURCE_EXHAUSTED)
        client.langgraph_service.PutCheckpoint.side_effect = error
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(grpc.aio.AioRpcError) as caught:
            await checkpointer.aput(config, checkpoint, metadata, {})

        assert caught.value is error
        assert client.langgraph_service.PutCheckpoint.await_count == 3
        assert mock_sleep.await_count == 2

    async def test_aput_does_not_retry_non_transient_status(self, client, mock_serde, config, checkpoint, metadata):
        error = _rpc_error(grpc.StatusCode.INVALID_ARGUMENT)
        client.langgraph_service.PutCheckpoint.side_effect = error
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(grpc.aio.AioRpcError) as caught:
            await checkpointer.aput(config, checkpoint, metadata, {})

        assert caught.value is error
        assert client.langgraph_service.PutCheckpoint.await_count == 1

    async def test_aput_propagates_cancelled_error(self, client, mock_serde, config, checkpoint, metadata):
        client.langgraph_service.PutCheckpoint.side_effect = asyncio.CancelledError()
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(asyncio.CancelledError):
            await checkpointer.aput(config, checkpoint, metadata, {})

        assert client.langgraph_service.PutCheckpoint.await_count == 1


class TestAputWritesRetry:
    """Tests for aput_writes retry logic."""

    async def test_aput_writes_sends_generated_request(self, client, mock_serde, config):
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        await checkpointer.aput_writes(config, [("channel", "value")], task_id="task-1")

        request = client.langgraph_service.PutWrites.await_args.args[0]
        assert isinstance(request, langgraph_pb2.PutWritesRequest)
        assert request.writes.thread_id == "test-thread"
        assert request.writes.checkpoint_id == "chk-parent"
        assert request.writes.task_id == "task-1"
        assert len(request.writes.writes) == 1
        assert request.writes.writes[0].channel == "channel"
        assert request.writes.writes[0].value == b'\x00{"fake": true}\xff'
        client.call_options.assert_awaited_once_with("admin@kagent.dev")

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_writes_retries_transient_rpc_error(self, mock_sleep, client, mock_serde, config):
        client.langgraph_service.PutWrites.side_effect = [
            _rpc_error(grpc.StatusCode.UNAVAILABLE),
            langgraph_pb2.PutWritesResponse(),
        ]
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        await checkpointer.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert client.langgraph_service.PutWrites.await_count == 2
        assert mock_sleep.await_count == 1

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_writes_raises_after_all_retries(self, mock_sleep, client, mock_serde, config):
        error = _rpc_error(grpc.StatusCode.UNAVAILABLE)
        client.langgraph_service.PutWrites.side_effect = error
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(grpc.aio.AioRpcError) as caught:
            await checkpointer.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert caught.value is error
        assert client.langgraph_service.PutWrites.await_count == 3
        assert mock_sleep.await_count == 2

    async def test_aput_writes_does_not_retry_non_transient_status(self, client, mock_serde, config):
        error = _rpc_error(grpc.StatusCode.PERMISSION_DENIED)
        client.langgraph_service.PutWrites.side_effect = error
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(grpc.aio.AioRpcError) as caught:
            await checkpointer.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert caught.value is error
        assert client.langgraph_service.PutWrites.await_count == 1

    async def test_aput_writes_propagates_cancelled_error(self, client, mock_serde, config):
        client.langgraph_service.PutWrites.side_effect = asyncio.CancelledError()
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(asyncio.CancelledError):
            await checkpointer.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert client.langgraph_service.PutWrites.await_count == 1

    async def test_aput_writes_requires_checkpoint_id(self, client, mock_serde):
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(ValueError, match="checkpoint_id is required"):
            await checkpointer.aput_writes(
                {"configurable": {"thread_id": "t1"}},
                [("ch", "val")],
                task_id="task-1",
            )

        client.langgraph_service.PutWrites.assert_not_awaited()


def _checkpoint_tuple(*, checkpoint_id: str = "chk-1") -> langgraph_pb2.LangGraphCheckpointTuple:
    checkpoint = langgraph_pb2.LangGraphCheckpoint(
        thread_id="test-thread",
        checkpoint_ns="",
        checkpoint_id=checkpoint_id,
        parent_checkpoint_id="chk-parent",
        checkpoint=b"checkpoint-bytes",
        metadata=b'{"source":"stored"}',
        type="checkpoint-type",
        version=1,
    )
    return langgraph_pb2.LangGraphCheckpointTuple(
        checkpoint=checkpoint,
        writes=langgraph_pb2.LangGraphCheckpointWrites(
            thread_id="test-thread",
            checkpoint_id=checkpoint_id,
            task_id="legacy-task",
            writes=[
                langgraph_pb2.LangGraphCheckpointWrite(
                    idx=0,
                    channel="messages",
                    type="write-type-a",
                    value=b"write-a",
                    task_id="task-a",
                ),
                langgraph_pb2.LangGraphCheckpointWrite(
                    idx=1,
                    channel="state",
                    type="write-type-b",
                    value=b"write-b",
                    task_id="task-b",
                ),
            ],
        ),
    )


class TestReads:
    async def test_aget_tuple_reconstructs_checkpoint_and_per_write_task_ids(self, client, mock_serde, config):
        client.langgraph_service.ListCheckpoints.return_value = langgraph_pb2.ListCheckpointsResponse(
            checkpoints=[_checkpoint_tuple()]
        )
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        result = await checkpointer.aget_tuple(config)

        assert result is not None
        request = client.langgraph_service.ListCheckpoints.await_args.args[0]
        assert request.thread_id == "test-thread"
        assert request.checkpoint_id == "chk-parent"
        assert request.limit == 1
        assert result.checkpoint == {"decoded": b"checkpoint-bytes"}
        assert result.metadata == {"source": "stored"}
        assert result.parent_config["configurable"]["checkpoint_id"] == "chk-parent"
        assert result.pending_writes == [
            ("task-a", "messages", {"decoded": b"write-a"}),
            ("task-b", "state", {"decoded": b"write-b"}),
        ]
        assert mock_serde.loads == [
            ("checkpoint-type", b"checkpoint-bytes"),
            ("write-type-a", b"write-a"),
            ("write-type-b", b"write-b"),
        ]
        client.call_options.assert_awaited_once_with("admin@kagent.dev")

    async def test_aget_tuple_updates_config_when_latest_checkpoint_is_requested(self, client, mock_serde):
        client.langgraph_service.ListCheckpoints.return_value = langgraph_pb2.ListCheckpointsResponse(
            checkpoints=[_checkpoint_tuple(checkpoint_id="latest")]
        )
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        result = await checkpointer.aget_tuple({"configurable": {"thread_id": "test-thread", "user_id": "user-1"}})

        assert result is not None
        assert result.config["configurable"]["checkpoint_id"] == "latest"
        request = client.langgraph_service.ListCheckpoints.await_args.args[0]
        assert not request.HasField("checkpoint_id")

    async def test_aget_tuple_maps_not_found_and_empty_results_to_none(self, client, mock_serde, config):
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)
        client.langgraph_service.ListCheckpoints.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND)

        assert await checkpointer.aget_tuple(config) is None

        client.langgraph_service.ListCheckpoints.reset_mock(side_effect=True)
        client.langgraph_service.ListCheckpoints.return_value = langgraph_pb2.ListCheckpointsResponse()
        assert await checkpointer.aget_tuple(config) is None

    async def test_alist_uses_generated_limit_and_yields_all_tuples(self, client, mock_serde, config):
        client.langgraph_service.ListCheckpoints.return_value = langgraph_pb2.ListCheckpointsResponse(
            checkpoints=[_checkpoint_tuple(checkpoint_id="one"), _checkpoint_tuple(checkpoint_id="two")]
        )
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        results = [item async for item in checkpointer.alist(config, limit=2)]

        assert len(results) == 2
        request = client.langgraph_service.ListCheckpoints.await_args.args[0]
        assert request.thread_id == "test-thread"
        assert request.limit == 2

    async def test_alist_requires_config(self, client, mock_serde):
        checkpointer = KAgentCheckpointer(client=client, app_name="test", serde=mock_serde)

        with pytest.raises(ValueError, match="config is required"):
            _ = [item async for item in checkpointer.alist(None)]
