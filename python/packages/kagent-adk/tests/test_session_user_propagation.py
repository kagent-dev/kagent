"""Integration tests for user_id propagation in session management.

These tests verify that the X-User-ID header is properly sent when
agents make HTTP calls to the KAgent controller for session operations.
"""

from unittest.mock import AsyncMock, MagicMock, Mock, patch

import httpx
import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import Message, Part, Role, TextPart
from google.adk.runners import Runner
from google.adk.sessions import Session

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._session_service import KAgentSessionService
from kagent.core.a2a import get_current_user_id, set_current_user_id


class TestSessionUserPropagation:
    """Test that user_id is properly propagated when creating/getting sessions."""

    @pytest.mark.asyncio
    async def test_session_service_receives_user_id_header(self):
        """Test that session service sends X-User-ID header in HTTP calls."""
        # Track HTTP requests
        captured_requests = []

        # Mock httpx.AsyncClient to capture requests
        async def mock_post(url, **kwargs):
            captured_requests.append(("POST", url, kwargs.get("headers", {})))
            response = Mock()
            response.status_code = 200
            response.json.return_value = {
                "data": {
                    "id": "test_session_id",
                    "user_id": "admin@kagent.dev",
                }
            }
            return response

        async def mock_get(url, **kwargs):
            captured_requests.append(("GET", url, kwargs.get("headers", {})))
            # Simulate session not found
            response = Mock()
            response.status_code = 404
            response.raise_for_status.side_effect = httpx.HTTPStatusError(
                "Not found", request=Mock(), response=response
            )
            return response

        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post = mock_post
        mock_client.get = mock_get

        # Create session service with mocked client
        session_service = KAgentSessionService(mock_client)

        # Set user_id in context (as agent executor would do)
        set_current_user_id("admin@kagent.dev")

        # Try to get session (will fail and then create)
        try:
            await session_service.get_session(
                app_name="test-app",
                user_id="admin@kagent.dev",
                session_id="test-session",
            )
        except httpx.HTTPStatusError:
            pass  # Expected - session doesn't exist

        # Create session
        session = await session_service.create_session(
            app_name="test-app",
            user_id="admin@kagent.dev",
            session_id="test-session",
        )

        # Verify requests were made
        assert len(captured_requests) == 2, "Should have made 2 requests (GET + POST)"

        # Verify GET request had X-User-ID header
        get_method, get_url, get_headers = captured_requests[0]
        assert get_method == "GET"
        assert "X-User-ID" in get_headers, "GET request should include X-User-ID header"
        assert get_headers["X-User-ID"] == "admin@kagent.dev"

        # Verify POST request had X-User-ID header
        post_method, post_url, post_headers = captured_requests[1]
        assert post_method == "POST"
        assert "X-User-ID" in post_headers, "POST request should include X-User-ID header"
        assert post_headers["X-User-ID"] == "admin@kagent.dev"

        # Verify session was created with correct user_id
        assert session.user_id == "admin@kagent.dev"

    @pytest.mark.asyncio
    async def test_agent_executor_sets_user_id_before_session_prep(self):
        """Test that agent executor sets user_id BEFORE preparing session.

        This is the key fix - user_id must be set in the context variable
        BEFORE any HTTP calls are made to the controller, so that the
        event hook can inject the X-User-ID header.
        """
        # Track the order of operations and captured state
        operation_log = []
        captured_user_id_during_session_prep = None

        # Create a mock session service that captures the context variable state
        async def mock_get_session(**kwargs):
            nonlocal captured_user_id_during_session_prep
            operation_log.append("session_service.get_session")
            # Capture what user_id is in the context variable at this moment
            captured_user_id_during_session_prep = get_current_user_id()

            mock_session = Mock(spec=Session)
            mock_session.id = "test_session"
            mock_session.user_id = kwargs.get("user_id")
            return mock_session

        # Create mock runner
        def runner_factory():
            mock_runner = MagicMock(spec=Runner)
            mock_runner.app_name = "test_app"

            # Mock session service
            mock_session_service = MagicMock()
            mock_session_service.get_session = mock_get_session
            mock_runner.session_service = mock_session_service

            # Mock invocation context
            mock_runner._new_invocation_context = MagicMock()

            # Mock run_async to return empty async iterator
            mock_runner.run_async = MagicMock(return_value=AsyncIteratorMock([]))

            return mock_runner

        # Create executor
        executor = A2aAgentExecutor(runner=runner_factory)

        # Create mock request context with a user
        mock_user = Mock()
        mock_user.user_name = "admin@kagent.dev"
        mock_call_context = Mock()
        mock_call_context.user = mock_user

        context = MagicMock(spec=RequestContext)
        context.call_context = mock_call_context
        context.context_id = "ctx-test-123"
        context.task_id = "task-test-456"
        context.message = Message(
            messageId="msg-test",
            role=Role.user,
            parts=[Part(root=TextPart(text="test message"))],
        )

        event_queue = MagicMock(spec=EventQueue)
        event_queue.enqueue_event = AsyncMock()

        # Clear context variable
        set_current_user_id(None)

        # Execute the request
        runner = runner_factory()
        await executor._handle_request(context, event_queue, runner)

        # CRITICAL ASSERTION: Verify that user_id was set BEFORE session preparation
        assert "session_service.get_session" in operation_log
        assert captured_user_id_during_session_prep == "admin@kagent.dev", (
            f"Expected user_id to be 'admin@kagent.dev' during session preparation, "
            f"but got '{captured_user_id_during_session_prep}'. "
            f"This means the context variable was not set before HTTP calls were made, "
            f"causing the controller to fall back to A2A_USER_* instead of using the real user."
        )

    @pytest.mark.asyncio
    async def test_httpx_event_hook_injects_header_from_context(self):
        """Test that httpx event hook properly injects X-User-ID from context variable."""
        from kagent.core.a2a import create_user_propagating_httpx_client

        # Set user_id in context
        set_current_user_id("test-user@example.com")

        # Create client with user propagation
        client = create_user_propagating_httpx_client()

        # Verify event hook is attached
        assert "request" in client._event_hooks
        assert len(client._event_hooks["request"]) == 1

        # Create a mock request
        mock_request = Mock(spec=httpx.Request)
        mock_request.headers = {}

        # Manually trigger the event hook (as httpx would)
        hook = client._event_hooks["request"][0]
        await hook(mock_request)

        # Verify header was injected
        assert "X-User-ID" in mock_request.headers
        assert mock_request.headers["X-User-ID"] == "test-user@example.com"

    @pytest.mark.asyncio
    async def test_context_variable_cleared_between_requests(self):
        """Test that context variable doesn't leak between requests."""
        # Request 1: Set user_id
        set_current_user_id("user1@example.com")
        assert get_current_user_id() == "user1@example.com"

        # Request 2: Set different user_id
        set_current_user_id("user2@example.com")
        assert get_current_user_id() == "user2@example.com"

        # Clear context
        set_current_user_id(None)
        assert get_current_user_id() is None


class TestSessionServiceHeaderInjection:
    """Test that KAgentSessionService explicitly sets X-User-ID header."""

    @pytest.mark.asyncio
    async def test_create_session_includes_user_id_header(self):
        """Verify create_session explicitly includes X-User-ID header."""
        captured_headers = {}

        async def mock_post(url, **kwargs):
            captured_headers.update(kwargs.get("headers", {}))
            response = Mock()
            response.status_code = 200
            response.json.return_value = {"data": {"id": "session-123", "user_id": "admin@kagent.dev"}}
            return response

        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.post = mock_post

        service = KAgentSessionService(mock_client)

        await service.create_session(
            app_name="test-app",
            user_id="admin@kagent.dev",
            session_id="session-123",
        )

        # Verify X-User-ID header was explicitly set
        assert "X-User-ID" in captured_headers
        assert captured_headers["X-User-ID"] == "admin@kagent.dev"

    @pytest.mark.asyncio
    async def test_get_session_includes_user_id_header(self):
        """Verify get_session explicitly includes X-User-ID header."""
        captured_headers = {}

        async def mock_get(url, **kwargs):
            captured_headers.update(kwargs.get("headers", {}))
            response = Mock()
            response.status_code = 200
            response.json.return_value = {"data": {"id": "session-123", "user_id": "admin@kagent.dev", "state": {}}}
            return response

        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_client.get = mock_get

        service = KAgentSessionService(mock_client)

        await service.get_session(
            app_name="test-app",
            user_id="admin@kagent.dev",
            session_id="session-123",
        )

        # Verify X-User-ID header was explicitly set
        assert "X-User-ID" in captured_headers
        assert captured_headers["X-User-ID"] == "admin@kagent.dev"


# Helper class for async iteration
class AsyncIteratorMock:
    """Mock async iterator for testing."""

    def __init__(self, items):
        self.items = items
        self.index = 0

    def __aiter__(self):
        return self

    async def __anext__(self):
        if self.index >= len(self.items):
            raise StopAsyncIteration
        item = self.items[self.index]
        self.index += 1
        return item

    async def aclose(self):
        """Mock aclose method."""
        pass
