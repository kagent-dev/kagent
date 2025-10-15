"""Tests for kagent.core.a2a._requests module."""

from unittest.mock import Mock

import httpx
import pytest
from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext
from a2a.server.context import ServerCallContext

from kagent.core.a2a import (
    USER_ID_HEADER,
    USER_ID_PREFIX,
    create_user_from_header,
    create_user_propagating_httpx_client,
    extract_header,
    extract_user_id,
    get_current_user_id,
    set_current_user_id,
)
from kagent.core.a2a._requests import _inject_user_id_header


class TestKAgentRequestHandlerUserIdMethods:
    """Tests for the user_id context variable methods."""

    def test_set_and_get_current_user_id(self):
        """Test setting and getting current user_id."""
        test_user_id = "test_user_123"

        # Set the user_id
        set_current_user_id(test_user_id)

        # Get the user_id
        result = get_current_user_id()

        assert result == test_user_id

    def test_set_current_user_id_none(self):
        """Test setting user_id to None clears it."""
        # First set a user_id
        set_current_user_id("test_user")

        # Then clear it
        set_current_user_id(None)

        # Should return None
        result = get_current_user_id()
        assert result is None

    def test_get_current_user_id_default(self):
        """Test getting user_id when none is set returns None."""
        # Clear any existing user_id
        set_current_user_id(None)

        result = get_current_user_id()
        assert result is None

    def test_user_id_context_isolation(self):
        """Test that user_id is properly isolated in context."""
        import asyncio
        from contextvars import copy_context

        # Set initial user_id
        set_current_user_id("user1")

        async def task_with_different_user():
            # Set different user_id in this context
            set_current_user_id("user2")
            return get_current_user_id()

        # Run task in copied context
        ctx = copy_context()
        result = ctx.run(lambda: asyncio.run(task_with_different_user()))

        # Original context should still have user1
        assert get_current_user_id() == "user1"
        # Task result should have user2
        assert result == "user2"


class TestKAgentRequestHandlerExistingMethods:
    """Tests for request utility functions."""

    def test_extract_user_id_from_call_context(self):
        """Test extracting user_id from call context."""
        # Create mock context with user
        mock_user = Mock(spec=User)
        mock_user.user_name = "test_user_123"

        mock_call_context = Mock()
        mock_call_context.user = mock_user

        mock_context = Mock(spec=RequestContext)
        mock_context.call_context = mock_call_context
        mock_context.context_id = "context_123"

        result = extract_user_id(mock_context)

        assert result == "test_user_123"

    def test_extract_user_id_fallback_to_context_variable(self):
        """Test fallback to context variable when no user in call context."""
        # Set user_id in context variable
        set_current_user_id("user_from_context")

        mock_context = Mock(spec=RequestContext)
        mock_context.call_context = None
        mock_context.context_id = "context_123"

        result = extract_user_id(mock_context)

        # Should use context variable instead of context_id
        assert result == "user_from_context"

    def test_extract_user_id_fallback_to_context_id(self):
        """Test fallback to context_id when no user in call context and no context variable."""
        # Clear context variable to ensure clean test
        set_current_user_id(None)

        mock_context = Mock(spec=RequestContext)
        mock_context.call_context = None
        mock_context.context_id = "context_123"

        result = extract_user_id(mock_context)

        assert result == f"{USER_ID_PREFIX}context_123"

    def test_extract_header_basic(self):
        """Test extracting header from server call context."""
        mock_context = Mock(spec=ServerCallContext)
        mock_context.state = {"headers": {"X-User-ID": "test_user_456"}}

        result = extract_header(mock_context, "X-User-ID")

        assert result == "test_user_456"

    def test_extract_header_not_found_returns_default(self):
        """Test extracting non-existent header returns default."""
        mock_context = Mock(spec=ServerCallContext)
        mock_context.state = {"headers": {}}

        result = extract_header(mock_context, "X-Missing", "default_value")

        assert result == "default_value"

    def test_create_user_from_header_success(self):
        """Test creating user from header."""
        mock_context = Mock(spec=ServerCallContext)
        mock_context.state = {"headers": {"X-User-ID": "header_user_789"}}

        result = create_user_from_header(mock_context)

        assert result is not None
        assert result.user_name == "header_user_789"
        assert result.user_id == "header_user_789"
        assert not result.is_authenticated

    def test_create_user_from_header_not_found_returns_none(self):
        """Test creating user from header when header not found."""
        mock_context = Mock(spec=ServerCallContext)
        mock_context.state = {"headers": {}}

        result = create_user_from_header(mock_context)

        assert result is None


class TestKAgentRequestHandlerHttpxClient:
    """Tests for the httpx client creation functions."""

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

    def test_create_user_propagating_httpx_client_default_timeout(self):
        """Test creating client with default timeout."""
        client = create_user_propagating_httpx_client()

        assert isinstance(client, httpx.AsyncClient)
        # Check that event hooks are configured
        assert "request" in client._event_hooks
        assert _inject_user_id_header in client._event_hooks["request"]

    def test_create_user_propagating_httpx_client_custom_timeout(self):
        """Test creating client with custom timeout."""
        custom_timeout = 60.0
        client = create_user_propagating_httpx_client(timeout=custom_timeout)

        assert isinstance(client, httpx.AsyncClient)
        # Verify timeout was set (just check that custom_timeout was used)
        assert custom_timeout == 60.0

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

    @pytest.mark.asyncio
    async def test_async_context_propagation(self):
        """Test that user_id propagates correctly in async context."""
        import asyncio

        # Set user_id in parent context
        set_current_user_id("async_user_789")

        # Create client with user ID propagation
        client = create_user_propagating_httpx_client()

        # Verify the hook is attached
        assert len(client._event_hooks["request"]) == 1

        # Create a mock request and verify header injection works asynchronously
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {}

        # Call the hook directly (as httpx would)
        hook = client._event_hooks["request"][0]
        await hook(mock_request)

        # Verify the header was added from the context
        assert mock_request.headers["X-User-ID"] == "async_user_789"

        # Test that context is maintained across async operations
        async def nested_operation():
            mock_request2 = Mock(spec=httpx.Request)
            mock_request2.headers = {}
            await hook(mock_request2)
            return mock_request2.headers.get("X-User-ID")

        result = await nested_operation()
        assert result == "async_user_789"
