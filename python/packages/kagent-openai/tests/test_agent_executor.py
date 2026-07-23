"""Tests for OpenAIAgentExecutor session handling."""

import uuid
from unittest.mock import AsyncMock, MagicMock, patch

from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import Message, Part, Role, TextPart

from kagent.openai._agent_executor import OpenAIAgentExecutor


def _make_context() -> MagicMock:
    context = MagicMock(spec=RequestContext)
    context.message = Message(
        message_id=str(uuid.uuid4()),
        role=Role.user,
        parts=[Part(TextPart(text="hello"))],
    )
    context.current_task = None
    context.task_id = "task-1"
    context.context_id = "ctx-1"
    context.session_id = None
    context.get_user_input.return_value = "hello"
    return context


def _make_streamed_result() -> MagicMock:
    async def _stream_events():
        for _ in ():
            yield None

    result = MagicMock()
    result.stream_events = _stream_events
    result.final_output = "done"
    return result


class TestLocalModeSession:
    """The executor built by build_local() has session_factory=None."""

    @patch("kagent.openai._agent_executor.Runner.run_streamed")
    async def test_execute_without_session_factory_does_not_raise(self, mock_run_streamed):
        mock_run_streamed.return_value = _make_streamed_result()

        executor = OpenAIAgentExecutor(agent=MagicMock(), app_name="test", session_factory=None)
        context = _make_context()
        event_queue = AsyncMock(spec=EventQueue)

        await executor.execute(context, event_queue)

        mock_run_streamed.assert_called_once()

    @patch("kagent.openai._agent_executor.Runner.run_streamed")
    async def test_session_context_falls_back_to_context_id(self, mock_run_streamed):
        mock_run_streamed.return_value = _make_streamed_result()

        executor = OpenAIAgentExecutor(agent=MagicMock(), app_name="test", session_factory=None)
        context = _make_context()
        event_queue = AsyncMock(spec=EventQueue)

        await executor.execute(context, event_queue)

        session_context = mock_run_streamed.call_args.kwargs["context"]
        assert session_context.session_id == context.context_id

    @patch("kagent.openai._agent_executor.Runner.run_streamed")
    async def test_session_context_uses_context_session_id(self, mock_run_streamed):
        mock_run_streamed.return_value = _make_streamed_result()

        executor = OpenAIAgentExecutor(agent=MagicMock(), app_name="test", session_factory=None)
        context = _make_context()
        context.session_id = "session-1"
        event_queue = AsyncMock(spec=EventQueue)

        await executor.execute(context, event_queue)

        session_context = mock_run_streamed.call_args.kwargs["context"]
        assert session_context.session_id == context.session_id
