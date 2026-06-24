"""Tests for share link tools."""

from unittest.mock import AsyncMock, MagicMock, patch

import httpx

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


class MockToolContext:
    """Mock ToolContext for testing."""

    def __init__(self, session_id: str = "test-session-123", app_name: str = "kagent__NS__myagent"):
        self.session = MockSession(session_id, app_name)


def _mock_response(status_code: int, json_data: object):
    """Build a mock httpx.Response."""
    r = MagicMock()
    r.status_code = status_code
    r.json.return_value = json_data
    r.text = str(json_data)
    return r


def _mock_client(**method_responses) -> AsyncMock:
    """Build a mock httpx.AsyncClient with the given method return values."""
    client = AsyncMock(spec=httpx.AsyncClient)
    for method, response in method_responses.items():
        getattr(client, method).return_value = response
    return client


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
        client = _mock_client(post=_mock_response(201, {"data": {"token": "tok-ro"}}))
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert "tok-ro" in result
        assert "(read-only)" in result

    async def test_creates_link_read_write(self):
        """args={'read_only': False} produces a read-write share link."""
        client = _mock_client(post=_mock_response(201, {"data": {"token": "tok-rw"}}))
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"read_only": False}, tool_context=ctx)

        assert "tok-rw" in result
        assert "(read-only)" not in result

    async def test_api_error(self):
        """A non-201 status code returns a failure message."""
        client = _mock_client(post=_mock_response(500, {"error": "internal server error"}))
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert result.startswith("Failed to create share link")

    async def test_sends_correct_read_only_in_body(self):
        """Default args send read_only=True in the request body."""
        client = _mock_client(post=_mock_response(201, {"data": {"token": "t"}}))
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        await tool.run_async(args={}, tool_context=ctx)

        client.post.assert_called_once()
        _, kwargs = client.post.call_args.args, client.post.call_args.kwargs
        assert kwargs.get("json") == {"read_only": True}

    async def test_sends_read_write_in_body(self):
        """args={'read_only': False} sends read_only=False in the request body."""
        client = _mock_client(post=_mock_response(201, {"data": {"token": "t"}}))
        tool = CreateShareLinkTool(client)
        ctx = MockToolContext()

        await tool.run_async(args={"read_only": False}, tool_context=ctx)

        client.post.assert_called_once()
        _, kwargs = client.post.call_args.args, client.post.call_args.kwargs
        assert kwargs.get("json") == {"read_only": False}


# ---------------------------------------------------------------------------
# ListShareLinksTool
# ---------------------------------------------------------------------------


class TestListShareLinksTool:
    """Tests for ListShareLinksTool.run_async."""

    async def test_returns_formatted_list(self):
        """A non-empty share list is returned with each token shown."""
        shares = [
            {"token": "tok-1", "created_at": "2024-01-01T00:00:00Z"},
            {"token": "tok-2", "created_at": "2024-01-02T00:00:00Z"},
        ]
        client = _mock_client(get=_mock_response(200, {"data": shares}))
        tool = ListShareLinksTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert "tok-1" in result
        assert "tok-2" in result

    async def test_empty_list(self):
        """An empty data list returns the 'no active share links' message."""
        client = _mock_client(get=_mock_response(200, {"data": []}))
        tool = ListShareLinksTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={}, tool_context=ctx)

        assert result == "No active share links for this session."

    async def test_api_error(self):
        """A non-200 status code returns a failure message."""
        client = _mock_client(get=_mock_response(404, {"error": "not found"}))
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
        """A successful DELETE returns a message containing 'revoked'."""
        client = _mock_client(delete=_mock_response(200, {"data": {}}))
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": "abc123"}, tool_context=ctx)

        assert "revoked" in result

    async def test_empty_token(self):
        """An empty token returns the 'token is required' error without an API call."""
        client = _mock_client()
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": ""}, tool_context=ctx)

        assert result == "Error: token is required."
        client.delete.assert_not_called()

    async def test_api_error(self):
        """A non-200 status code returns a failure message."""
        client = _mock_client(delete=_mock_response(403, {"error": "forbidden"}))
        tool = DeleteShareLinkTool(client)
        ctx = MockToolContext()

        result = await tool.run_async(args={"token": "abc123"}, tool_context=ctx)

        assert result.startswith("Failed")
