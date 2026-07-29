"""Tests for share link tools."""

from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import grpc
from kagent.api.v1alpha1 import sessions_pb2

from kagent.adk.tools.share_tools import (
    CreateShareLinkTool,
    DeleteShareLinkTool,
    ListShareLinksTool,
    _parse_app_name,
    _share_url,
)


class MockSession:
    """Mock Session for testing."""

    def __init__(self, session_id: str = "test-session-123", app_name: str = "kagent__NS__myagent"):
        self.id = session_id
        self.app_name = app_name
        self.user_id = "user-1"


class MockToolContext:
    """Mock ToolContext for testing."""

    def __init__(self, session_id: str = "test-session-123", app_name: str = "kagent__NS__myagent"):
        self.session = MockSession(session_id, app_name)


def _mock_client() -> MagicMock:
    """Build an AsyncControllerClient-shaped mock."""
    client = MagicMock()
    client.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
    client.session_service = MagicMock()
    client.session_service.CreateSessionShare = AsyncMock()
    client.session_service.ListSessionShares = AsyncMock()
    client.session_service.DeleteSessionShare = AsyncMock(
        return_value=sessions_pb2.DeleteSessionShareResponse()
    )
    return client


def _rpc_error(code: grpc.StatusCode, details: str) -> grpc.aio.AioRpcError:
    return grpc.aio.AioRpcError(code, (), (), details, "")


# ---------------------------------------------------------------------------
# _parse_app_name
# ---------------------------------------------------------------------------


class TestParseAppName:
    """Tests for _parse_app_name."""

    def test_standard_format(self):
        """kagent__NS__my_agent → ('kagent', 'my-agent')."""
        ns, name = _parse_app_name("kagent__NS__my_agent")
        assert ns == "kagent"
        assert name == "my-agent"

    def test_no_separator(self):
        """app_name with no __NS__ separator returns empty namespace."""
        ns, name = _parse_app_name("noformat")
        assert ns == ""
        assert name == "noformat"


# ---------------------------------------------------------------------------
# _share_url
# ---------------------------------------------------------------------------


class TestShareUrl:
    """Tests for _share_url."""

    def test_with_ui_url(self):
        """With KAGENT_UI_URL set, returns an absolute URL."""
        with patch("kagent.adk.tools.share_tools._KAGENT_UI_URL", "https://example.com"):
            url = _share_url("abc123", "sess-1", "kagent__NS__myagent")
        assert url.startswith("https://example.com")
        assert "abc123" in url
        assert "sess-1" in url

    def test_without_ui_url(self):
        """Without KAGENT_UI_URL, returns a relative path."""
        with patch("kagent.adk.tools.share_tools._KAGENT_UI_URL", ""):
            url = _share_url("abc123", "sess-1", "kagent__NS__myagent")
        assert url.startswith("/")
        assert "abc123" in url


# ---------------------------------------------------------------------------
# CreateShareLinkTool
# ---------------------------------------------------------------------------


class TestCreateShareLinkTool:
    """Tests for CreateShareLinkTool.run_async."""

    async def test_creates_link_read_only_by_default(self):
        """Default args produce a read-only share link."""
        client = _mock_client()
        client.session_service.CreateSessionShare.return_value = sessions_pb2.CreateSessionShareResponse(
            share=sessions_pb2.SessionShare(token="tok-ro", read_only=True)
        )
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert "tok-ro" in result
        assert "(read-only)" in result

    async def test_creates_link_read_write(self):
        """args={'read_only': False} produces a read-write share link."""
        client = _mock_client()
        client.session_service.CreateSessionShare.return_value = sessions_pb2.CreateSessionShareResponse(
            share=sessions_pb2.SessionShare(token="tok-rw", read_only=False)
        )
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"read_only": False}, tool_context=ctx)

        assert "tok-rw" in result
        assert "(read-only)" not in result

    async def test_api_error(self):
        """A canonical RPC error returns a failure message."""
        client = _mock_client()
        client.session_service.CreateSessionShare.side_effect = _rpc_error(
            grpc.StatusCode.INTERNAL, "internal server error"
        )
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert result.startswith("Failed to create share link")

    async def test_sends_correct_read_only_in_request(self):
        """Default args send read_only=True in the generated request."""
        client = _mock_client()
        client.session_service.CreateSessionShare.return_value = sessions_pb2.CreateSessionShareResponse(
            share=sessions_pb2.SessionShare(token="t", read_only=True)
        )
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        await tool.run_async(args={}, tool_context=ctx)

        request = client.session_service.CreateSessionShare.await_args.args[0]
        assert request.session_id == "test-session-123"
        assert request.HasField("read_only")
        assert request.read_only is True
        client.call_options.assert_awaited_once_with("user-1")

    async def test_sends_read_write_in_request(self):
        """args={'read_only': False} sends read_only=False in the generated request."""
        client = _mock_client()
        client.session_service.CreateSessionShare.return_value = sessions_pb2.CreateSessionShareResponse(
            share=sessions_pb2.SessionShare(token="t", read_only=False)
        )
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        await tool.run_async(args={"read_only": False}, tool_context=ctx)

        request = client.session_service.CreateSessionShare.await_args.args[0]
        assert request.HasField("read_only")
        assert request.read_only is False


# ---------------------------------------------------------------------------
# ListShareLinksTool
# ---------------------------------------------------------------------------


class TestListShareLinksTool:
    """Tests for ListShareLinksTool.run_async."""

    async def test_returns_formatted_list(self):
        """A non-empty share list is returned with each token shown."""
        shares = [
            sessions_pb2.SessionShare(token="tok-1", created_at=datetime(2024, 1, 1, tzinfo=timezone.utc)),
            sessions_pb2.SessionShare(token="tok-2", created_at=datetime(2024, 1, 2, tzinfo=timezone.utc)),
        ]
        client = _mock_client()
        client.session_service.ListSessionShares.return_value = sessions_pb2.ListSessionSharesResponse(shares=shares)
        tool = ListShareLinksTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert "tok-1" in result
        assert "tok-2" in result
        request = client.session_service.ListSessionShares.await_args.args[0]
        assert request.session_id == "test-session-123"
        client.call_options.assert_awaited_once_with("user-1")

    async def test_empty_list(self):
        """An empty data list returns the 'no active share links' message."""
        client = _mock_client()
        client.session_service.ListSessionShares.return_value = sessions_pb2.ListSessionSharesResponse()
        tool = ListShareLinksTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert result == "No active share links for this session."

    async def test_api_error(self):
        """A canonical RPC error returns a failure message."""
        client = _mock_client()
        client.session_service.ListSessionShares.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND, "not found")
        tool = ListShareLinksTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert result.startswith("Failed")


# ---------------------------------------------------------------------------
# DeleteShareLinkTool
# ---------------------------------------------------------------------------


class TestDeleteShareLinkTool:
    """Tests for DeleteShareLinkTool.run_async."""

    async def test_revokes_token(self):
        """A successful delete RPC returns a message containing 'revoked'."""
        client = _mock_client()
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": "abc123"}, tool_context=ctx)

        assert "revoked" in result
        request = client.session_service.DeleteSessionShare.await_args.args[0]
        assert request.session_id == "test-session-123"
        assert request.token == "abc123"
        client.call_options.assert_awaited_once_with("user-1")

    async def test_empty_token(self):
        """An empty token returns the 'token is required' error without an API call."""
        client = _mock_client()
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": ""}, tool_context=ctx)

        assert result == "Error: token is required."
        client.session_service.DeleteSessionShare.assert_not_awaited()

    async def test_api_error(self):
        """A canonical RPC error returns a failure message."""
        client = _mock_client()
        client.session_service.DeleteSessionShare.side_effect = _rpc_error(
            grpc.StatusCode.PERMISSION_DENIED, "forbidden"
        )
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": "abc123"}, tool_context=ctx)

        assert result.startswith("Failed")
