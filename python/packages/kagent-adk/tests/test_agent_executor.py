"""Tests for the A2aAgentExecutor class."""

import asyncio
import uuid
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from google.adk.agents import Agent
from google.adk.events import Event
from google.adk.runners import Runner
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import BaseSessionService
from google.genai.types import Content

from kagent.adk._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig


class TestA2aAgentExecutorInit:
    """Test A2aAgentExecutor initialization."""

    def test_init_with_callable_runner(self):
        """Test initialization with a callable runner."""

        def runner_factory():
            return MagicMock(spec=Runner)

        config = A2aAgentExecutorConfig()
        executor = A2aAgentExecutor(runner=runner_factory, config=config)

        assert executor._runner == runner_factory
        assert executor._config == config

    def test_init_with_async_callable_runner(self):
        """Test initialization with an async callable runner."""

        async def async_runner_factory():
            return MagicMock(spec=Runner)

        executor = A2aAgentExecutor(runner=async_runner_factory)

        assert executor._runner == async_runner_factory
        assert executor._config is None


class TestResolveRunner:
    """Test _resolve_runner method."""

    @pytest.mark.asyncio
    async def test_resolve_runner_with_sync_callable(self):
        """Test resolving a synchronous callable that returns a Runner."""
        mock_runner = MagicMock(spec=Runner)

        def runner_factory():
            return mock_runner

        executor = A2aAgentExecutor(runner=runner_factory)
        resolved = await executor._resolve_runner()

        assert resolved == mock_runner

    @pytest.mark.asyncio
    async def test_resolve_runner_with_async_callable(self):
        """Test resolving an asynchronous callable that returns a Runner."""
        mock_runner = MagicMock(spec=Runner)

        async def async_runner_factory():
            return mock_runner

        executor = A2aAgentExecutor(runner=async_runner_factory)
        resolved = await executor._resolve_runner()

        assert resolved == mock_runner

    @pytest.mark.asyncio
    async def test_resolve_runner_with_non_runner_return(self):
        """Test that TypeError is raised when callable doesn't return a Runner."""

        def bad_factory():
            return "not a runner"

        executor = A2aAgentExecutor(runner=bad_factory)

        with pytest.raises(TypeError, match="Callable must return a Runner instance"):
            await executor._resolve_runner()

    @pytest.mark.asyncio
    async def test_resolve_runner_with_non_callable(self):
        """Test that TypeError is raised when runner is not callable."""
        executor = A2aAgentExecutor(runner="not callable")

        with pytest.raises(TypeError, match="Runner must be a Runner instance or a callable"):
            await executor._resolve_runner()


class TestCancel:
    """Test cancel method."""

    @pytest.mark.asyncio
    async def test_cancel_raises_not_implemented(self):
        """Test that cancel method raises NotImplementedError."""

        def runner_factory():
            return MagicMock(spec=Runner)

        executor = A2aAgentExecutor(runner=runner_factory)
        context = MagicMock(spec=RequestContext)
        event_queue = MagicMock(spec=EventQueue)

        with pytest.raises(NotImplementedError, match="Cancellation is not supported"):
            await executor.cancel(context, event_queue)


class TestExecute:
    """Test execute method."""

    @pytest.mark.asyncio
    async def test_execute_without_message_raises_error(self):
        """Test that execute raises ValueError when context has no message."""

        def runner_factory():
            return MagicMock(spec=Runner)

        executor = A2aAgentExecutor(runner=runner_factory)
        context = MagicMock(spec=RequestContext)
        context.message = None
        event_queue = AsyncMock(spec=EventQueue)

        with pytest.raises(ValueError, match="A2A request must have a message"):
            await executor.execute(context, event_queue)

    @pytest.mark.asyncio
    async def test_execute_creates_submitted_event_for_new_task(self):
        """Test that execute creates a submitted event for new tasks."""
        mock_runner = MagicMock(spec=Runner)
        mock_runner.app_name = "test-app"
        mock_runner.run_async = AsyncMock(return_value=AsyncIteratorMock([]))

        # Mock session service
        mock_session_service = AsyncMock(spec=BaseSessionService)
        mock_session = MagicMock(spec=Session)
        mock_session.id = "test-session-id"
        mock_session_service.get_session.return_value = mock_session
        mock_runner.session_service = mock_session_service
        mock_runner._new_invocation_context = MagicMock()

        def runner_factory():
            return mock_runner

        executor = A2aAgentExecutor(runner=runner_factory)

        context = MagicMock(spec=RequestContext)
        context.message = Message(message_id="test-msg", role=Role.user, parts=[Part(TextPart(text="Hello"))])
        context.current_task = None  # New task
        context.task_id = "test-task-id"
        context.context_id = "test-context-id"
        context.call_context = None

        event_queue = AsyncMock(spec=EventQueue)

        await executor.execute(context, event_queue)

        # Check that submitted event was enqueued
        calls = event_queue.enqueue_event.call_args_list
        assert len(calls) >= 1
        submitted_event = calls[0][0][0]
        assert isinstance(submitted_event, TaskStatusUpdateEvent)
        assert submitted_event.status.state == TaskState.submitted

    @pytest.mark.asyncio
    async def test_execute_handles_exception_and_publishes_failure(self):
        """Test that execute handles exceptions and publishes failure event."""
        mock_runner = MagicMock(spec=Runner)

        def failing_runner():
            return mock_runner

        executor = A2aAgentExecutor(runner=failing_runner)

        context = MagicMock(spec=RequestContext)
        context.message = Message(message_id="test-msg", role=Role.user, parts=[Part(TextPart(text="Hello"))])
        context.current_task = MagicMock()  # Existing task
        context.task_id = "test-task-id"
        context.context_id = "test-context-id"

        event_queue = AsyncMock(spec=EventQueue)

        # Mock _handle_request to raise an exception
        with patch.object(executor, "_handle_request", side_effect=ValueError("Test error")):
            await executor.execute(context, event_queue)

        # Check that failure event was enqueued
        calls = event_queue.enqueue_event.call_args_list
        assert len(calls) >= 1
        failure_event = calls[-1][0][0]
        assert isinstance(failure_event, TaskStatusUpdateEvent)
        assert failure_event.status.state == TaskState.failed
        assert failure_event.final is True


class TestHandleRequest:
    """Test _handle_request method."""

    @pytest.mark.asyncio
    async def test_handle_request_basic_flow(self):
        """Test basic flow of _handle_request."""
        # Skip detailed testing of _handle_request as it's complex and involves
        # many dependencies. Focus on integration tests instead.
        pass

    @pytest.mark.asyncio
    async def test_handle_request_sets_user_id_in_context(self):
        """Test that _handle_request sets user_id in context variable."""
        from unittest.mock import patch

        from kagent.core.a2a import get_current_user_id, set_current_user_id

        # Create executor
        def runner_factory():
            mock_runner = MagicMock(spec=Runner)
            mock_runner.app_name = "test_app"

            # Mock session service
            mock_session_service = MagicMock(spec=BaseSessionService)
            mock_session = MagicMock(spec=Session)
            mock_session.session_id = "test_session"
            mock_session_service.get_session.return_value = mock_session
            mock_runner.session_service = mock_session_service

            # Mock invocation context
            mock_invocation_context = MagicMock()
            mock_runner._new_invocation_context.return_value = mock_invocation_context

            # Mock run_async to return empty async iterator
            mock_runner.run_async.return_value = AsyncIteratorMock([])

            return mock_runner

        executor = A2aAgentExecutor(runner=runner_factory)

        # Create mock context with user
        mock_user = MagicMock()
        mock_user.user_name = "test_user_123"
        mock_call_context = MagicMock()
        mock_call_context.user = mock_user

        context = MagicMock(spec=RequestContext)
        context.call_context = mock_call_context
        context.context_id = "test_context"
        context.task_id = "test_task"
        context.message = Message(
            messageId="test_message_id", role=Role.user, parts=[Part(root=TextPart(text="test message"))]
        )

        event_queue = MagicMock(spec=EventQueue)
        event_queue.enqueue_event = AsyncMock()

        # Clear any existing user_id
        set_current_user_id(None)

        # Call _handle_request
        runner = runner_factory()
        await executor._handle_request(context, event_queue, runner)

        # Verify user_id was set in context
        current_user_id = get_current_user_id()
        assert current_user_id == "test_user_123"

    @pytest.mark.asyncio
    async def test_handle_request_sets_user_id_before_session_preparation(self):
        """Test that user_id is set BEFORE session is prepared (for HTTP header injection)."""
        from kagent.core.a2a import get_current_user_id, set_current_user_id

        # Track the order of operations
        operation_order = []
        captured_user_id_during_get_session = None

        # Create executor with mocked session service that captures user_id
        def runner_factory():
            mock_runner = MagicMock(spec=Runner)
            mock_runner.app_name = "test_app"

            # Mock session service that captures the context variable state
            async def mock_get_session(**kwargs):
                nonlocal captured_user_id_during_get_session
                operation_order.append("get_session")
                # Capture what the user_id context variable is at this moment
                captured_user_id_during_get_session = get_current_user_id()
                mock_session = MagicMock(spec=Session)
                mock_session.session_id = "test_session"
                return mock_session

            mock_session_service = MagicMock(spec=BaseSessionService)
            mock_session_service.get_session = mock_get_session
            mock_runner.session_service = mock_session_service

            # Mock invocation context
            mock_invocation_context = MagicMock()
            mock_runner._new_invocation_context.return_value = mock_invocation_context

            # Mock run_async to return empty async iterator
            mock_runner.run_async.return_value = AsyncIteratorMock([])

            return mock_runner

        executor = A2aAgentExecutor(runner=runner_factory)

        # Create mock context with user
        mock_user = MagicMock()
        mock_user.user_name = "admin@kagent.dev"
        mock_call_context = MagicMock()
        mock_call_context.user = mock_user

        context = MagicMock(spec=RequestContext)
        context.call_context = mock_call_context
        context.context_id = "test_context"
        context.task_id = "test_task"
        context.message = Message(
            messageId="test_message_id", role=Role.user, parts=[Part(root=TextPart(text="test message"))]
        )

        event_queue = MagicMock(spec=EventQueue)
        event_queue.enqueue_event = AsyncMock()

        # Clear any existing user_id
        set_current_user_id(None)

        # Call _handle_request
        runner = runner_factory()
        await executor._handle_request(context, event_queue, runner)

        # Verify that when get_session was called, user_id was already set
        assert operation_order == ["get_session"], "get_session should be called"
        assert captured_user_id_during_get_session == "admin@kagent.dev", (
            "user_id should be set in context BEFORE session preparation. "
            "This ensures X-User-ID header is injected in HTTP calls to KAgent controller."
        )


class TestPrepareSession:
    """Test _prepare_session method."""

    @pytest.mark.asyncio
    async def test_prepare_session_basic_flow(self):
        """Test basic flow of _prepare_session."""
        # Skip detailed testing as it's a private method with complex dependencies
        pass


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
