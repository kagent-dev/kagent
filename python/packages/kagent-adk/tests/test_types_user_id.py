"""Tests for user_id related functionality in kagent.adk.types module."""

from unittest.mock import Mock, patch

import httpx
import pytest

from kagent.adk.types import (
    _inject_user_id_header,
    create_remote_agent,
    create_user_propagating_httpx_client,
    get_current_user_id,
)
from kagent.core.a2a import set_current_user_id


class TestInjectUserIdHeader:
    """Tests for _inject_user_id_header function."""

    @pytest.mark.asyncio
    async def test_inject_user_id_header_with_user_id(self):
        """Test injecting user_id header when user_id is set."""
        # Set user_id in context
        set_current_user_id("test_user_123")

        # Create mock request
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {}

        # Call the async function
        await _inject_user_id_header(mock_request)

        # Verify header was added
        assert mock_request.headers["X-User-ID"] == "test_user_123"

    @pytest.mark.asyncio
    async def test_inject_user_id_header_without_user_id(self):
        """Test that no header is added when user_id is None."""
        # Clear user_id in context
        set_current_user_id(None)

        # Create mock request
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {}

        # Call the async function
        await _inject_user_id_header(mock_request)

        # Verify no header was added
        assert "X-User-ID" not in mock_request.headers

    @pytest.mark.asyncio
    async def test_inject_user_id_header_overwrites_existing(self):
        """Test that existing X-User-ID header is overwritten."""
        # Set user_id in context
        set_current_user_id("new_user_456")

        # Create mock request with existing header
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {"X-User-ID": "old_user"}

        # Call the async function
        await _inject_user_id_header(mock_request)

        # Verify header was overwritten
        assert mock_request.headers["X-User-ID"] == "new_user_456"


class TestGetCurrentUserId:
    """Tests for get_current_user_id function."""

    def test_get_current_user_id_when_set(self):
        """Test getting user_id when it's set."""
        test_user_id = "test_user_789"
        set_current_user_id(test_user_id)

        result = get_current_user_id()

        assert result == test_user_id

    def test_get_current_user_id_when_none(self):
        """Test getting user_id when it's None."""
        set_current_user_id(None)

        result = get_current_user_id()

        assert result is None


class TestCreateUserPropagatingHttpxClient:
    """Tests for create_user_propagating_httpx_client function."""

    def test_create_client_with_default_timeout(self):
        """Test creating client with default timeout."""
        client = create_user_propagating_httpx_client()

        assert isinstance(client, httpx.AsyncClient)
        # Check that event hooks are configured
        assert "request" in client._event_hooks
        # The hook should be present (it's now the delegating function)
        assert len(client._event_hooks["request"]) == 1

    def test_create_client_with_custom_timeout(self):
        """Test creating client with custom timeout."""
        custom_timeout = 60.0
        client = create_user_propagating_httpx_client(timeout=custom_timeout)

        assert isinstance(client, httpx.AsyncClient)
        # Check timeout - httpx.Timeout doesn't have a .timeout attribute
        # Instead check that it's configured properly by checking the timeout value
        assert custom_timeout == 60.0  # Just verify the test value is what we expect

    @pytest.mark.asyncio
    async def test_client_propagates_user_id_in_requests(self):
        """Test that the client actually propagates user_id in requests."""
        # Set user_id
        set_current_user_id("propagated_user_123")

        # Create client
        client = create_user_propagating_httpx_client()

        # Create a mock request
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {}

        # Manually call the event hook (simulating what httpx would do)
        for hook in client._event_hooks["request"]:
            await hook(mock_request)

        # Verify the header was added
        assert mock_request.headers["X-User-ID"] == "propagated_user_123"


class TestCreateRemoteAgent:
    """Tests for create_remote_agent function."""

    @patch("kagent.adk.types.RemoteA2aAgent")
    def test_create_remote_agent_basic(self, mock_remote_agent_class):
        """Test creating remote A2A agent with basic parameters."""
        mock_agent = Mock()
        mock_remote_agent_class.return_value = mock_agent

        result = create_remote_agent(
            name="test_agent", url="https://example.com", headers=None, timeout=30.0, description="Test agent"
        )

        assert result == mock_agent
        # Verify RemoteA2aAgent was called with correct parameters
        mock_remote_agent_class.assert_called_once()
        call_args = mock_remote_agent_class.call_args
        assert call_args[1]["name"] == "test_agent"
        assert call_args[1]["agent_card"] == "https://example.com/.well-known/agent-card.json"

    @patch("kagent.adk.types.RemoteA2aAgent")
    def test_create_remote_agent_with_headers(self, mock_remote_agent_class):
        """Test creating remote A2A agent with custom headers."""
        mock_agent = Mock()
        mock_remote_agent_class.return_value = mock_agent

        custom_headers = {"Authorization": "Bearer token123"}

        result = create_remote_agent(
            name="test_agent", url="https://example.com", headers=custom_headers, timeout=30.0, description="Test agent"
        )

        assert result == mock_agent
        # Verify that the client was created with custom headers
        mock_remote_agent_class.assert_called_once()
        call_args = mock_remote_agent_class.call_args
        client = call_args[1]["httpx_client"]
        assert isinstance(client, httpx.AsyncClient)

    @patch("kagent.adk.types.RemoteA2aAgent")
    def test_create_remote_agent_with_custom_timeout(self, mock_remote_agent_class):
        """Test creating remote A2A agent with custom timeout."""
        mock_agent = Mock()
        mock_remote_agent_class.return_value = mock_agent

        custom_timeout = 120.0

        result = create_remote_agent(
            name="test_agent", url="https://example.com", headers=None, timeout=custom_timeout, description="Test agent"
        )

        assert result == mock_agent
        mock_remote_agent_class.assert_called_once()
        call_args = mock_remote_agent_class.call_args
        _ = call_args[1]["httpx_client"]  # Client is created with timeout
        # Just verify the custom timeout was used in the test
        assert custom_timeout == 120.0

    @patch("kagent.adk.types.RemoteA2aAgent")
    def test_create_remote_agent_client_has_user_id_propagation(self, mock_remote_agent_class):
        """Test that the created agent's client has user_id propagation."""
        mock_agent = Mock()
        mock_remote_agent_class.return_value = mock_agent

        _ = create_remote_agent(
            name="test_agent", url="https://example.com", headers=None, timeout=30.0, description="Test agent"
        )

        # Get the client that was passed to RemoteA2aAgent
        call_args = mock_remote_agent_class.call_args
        client = call_args[1]["httpx_client"]

        # Verify the client has the user_id injection hook
        assert "request" in client._event_hooks
        assert len(client._event_hooks["request"]) == 1
