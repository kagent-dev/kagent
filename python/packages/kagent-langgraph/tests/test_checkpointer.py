"""Tests for KAgentCheckpointer retry logic."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest
from langgraph.checkpoint.serde.base import SerializerProtocol

from kagent.langgraph._checkpointer import KAgentCheckpointer


class FakeSerde(SerializerProtocol):
    """A fake serializer that satisfies the SerializerProtocol runtime check."""

    def dumps_typed(self, obj):
        return ("json", b'{"fake": true}')

    def loads_typed(self, data):
        return {"fake": True}


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
    return {}


def _make_success_response():
    resp = MagicMock(spec=httpx.Response)
    resp.status_code = 200
    resp.raise_for_status = MagicMock()
    return resp


def _make_error_response(status_code):
    resp = MagicMock(spec=httpx.Response)
    resp.status_code = status_code
    resp.raise_for_status.side_effect = httpx.HTTPStatusError(f"HTTP {status_code}", request=MagicMock(), response=resp)
    return resp


class TestAputRetry:
    """Tests for aput retry logic."""

    async def test_aput_succeeds_on_first_attempt(self, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.return_value = _make_success_response()

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        result = await cp.aput(config, checkpoint, metadata, {})

        assert result["configurable"]["checkpoint_id"] == "chk-1"
        assert mock_client.post.call_count == 1

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_retries_on_transport_error(self, mock_sleep, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = [
            httpx.ConnectError("connection refused"),
            httpx.ConnectError("connection refused"),
            _make_success_response(),
        ]

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        result = await cp.aput(config, checkpoint, metadata, {})

        assert result["configurable"]["checkpoint_id"] == "chk-1"
        assert mock_client.post.call_count == 3
        assert mock_sleep.call_count == 2

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_raises_after_all_retries_exhausted(self, mock_sleep, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = httpx.ConnectError("connection refused")

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(httpx.ConnectError):
            await cp.aput(config, checkpoint, metadata, {})

        assert mock_client.post.call_count == 3
        assert mock_sleep.call_count == 2

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_retries_on_5xx(self, mock_sleep, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = [
            _make_error_response(503),
            _make_success_response(),
        ]

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        result = await cp.aput(config, checkpoint, metadata, {})

        assert result["configurable"]["checkpoint_id"] == "chk-1"
        assert mock_client.post.call_count == 2

    async def test_aput_does_not_retry_on_4xx(self, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.return_value = _make_error_response(400)

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(httpx.HTTPStatusError):
            await cp.aput(config, checkpoint, metadata, {})

        assert mock_client.post.call_count == 1  # No retry

    async def test_aput_propagates_cancelled_error(self, mock_serde, config, checkpoint, metadata):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = asyncio.CancelledError()

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(asyncio.CancelledError):
            await cp.aput(config, checkpoint, metadata, {})

        assert mock_client.post.call_count == 1  # No retry


class TestAputWritesRetry:
    """Tests for aput_writes retry logic."""

    async def test_aput_writes_succeeds_on_first_attempt(self, mock_serde, config):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.return_value = _make_success_response()

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        await cp.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert mock_client.post.call_count == 1

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_writes_retries_on_transport_error(self, mock_sleep, mock_serde, config):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = [
            httpx.ConnectError("connection refused"),
            _make_success_response(),
        ]

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        await cp.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert mock_client.post.call_count == 2
        assert mock_sleep.call_count == 1

    @patch("asyncio.sleep", new_callable=AsyncMock)
    async def test_aput_writes_raises_after_all_retries(self, mock_sleep, mock_serde, config):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = httpx.ConnectError("connection refused")

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(httpx.ConnectError):
            await cp.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert mock_client.post.call_count == 3

    async def test_aput_writes_does_not_retry_on_4xx(self, mock_serde, config):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.return_value = _make_error_response(403)

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(httpx.HTTPStatusError):
            await cp.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert mock_client.post.call_count == 1

    async def test_aput_writes_propagates_cancelled_error(self, mock_serde, config):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post.side_effect = asyncio.CancelledError()

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(asyncio.CancelledError):
            await cp.aput_writes(config, [("channel", "value")], task_id="task-1")

        assert mock_client.post.call_count == 1

    async def test_aput_writes_requires_checkpoint_id(self, mock_serde):
        config_no_checkpoint = {"configurable": {"thread_id": "t1"}}
        mock_client = AsyncMock(spec=httpx.AsyncClient)

        cp = KAgentCheckpointer(client=mock_client, app_name="test", serde=mock_serde)
        with pytest.raises(ValueError, match="checkpoint_id is required"):
            await cp.aput_writes(config_no_checkpoint, [("ch", "val")], task_id="task-1")
