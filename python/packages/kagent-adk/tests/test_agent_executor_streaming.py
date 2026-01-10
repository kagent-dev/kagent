from unittest.mock import AsyncMock, Mock, patch

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import Message as A2AMessage
from a2a.types import TaskStatusUpdateEvent
from google.adk.events import Event, EventActions
from google.genai.types import Content, Part, TextPart

from kagent.adk._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig


@pytest.mark.asyncio
async def test_streaming_message_id_reuse():
    # Setup mocks
    mock_runner_instance = AsyncMock()
    mock_runner_instance.app_name = "test-app"
    mock_runner_instance.session_service.get_session.return_value = Mock(id="session-1")
    mock_runner_instance.session_service.create_session.return_value = Mock(id="session-1")
    mock_runner_instance._new_invocation_context.return_value = Mock(
        app_name="test-app", user_id="user-1", session=Mock(id="session-1")
    )

    mock_event_queue = AsyncMock()

    # Mock events (Partial 1 -> Partial 2 -> Final)
    event1 = Event(
        invocation_id="inv-1",
        author="agent",
        content=Content(parts=[Part(text="Hello ")]),
        partial=True,
    )

    event2 = Event(
        invocation_id="inv-1",
        author="agent",
        content=Content(parts=[Part(text="World")]),
        partial=True,
    )

    # Final event typically has full content
    event3 = Event(
        invocation_id="inv-1",
        author="agent",
        content=Content(parts=[Part(text="Hello World")]),
        partial=False,
    )

    async def mock_run_async(**kwargs):
        yield event1
        yield event2
        yield event3

    mock_runner_instance.run_async = mock_run_async

    # Initialize Executor
    executor = A2aAgentExecutor(runner=lambda: mock_runner_instance, config=A2aAgentExecutorConfig(stream=True))

    # Create Request Context
    context = RequestContext(
        message=A2AMessage(role="user", parts=[TextPart(text="Hi")]),
        task_id="task-1",
        context_id="ctx-1",
        call_context=Mock(state={}),
    )

    # Execute
    await executor.execute(context, mock_event_queue)

    # Verify events in queue
    # 0: Submitted
    # 1: Working (Initial)
    # 2: Streaming Chunk 1
    # 3: Streaming Chunk 2
    # 4: Streaming Final Update
    # 5: Completed (State update)

    # Filter for TaskStatusUpdateEvents that contain a message
    message_events = [
        call.args[0]
        for call in mock_event_queue.enqueue_event.call_args_list
        if isinstance(call.args[0], TaskStatusUpdateEvent) and call.args[0].status.message
    ]

    # We expect 3 message updates
    assert len(message_events) >= 3

    msg1 = message_events[0]
    msg2 = message_events[1]
    msg3 = message_events[2]

    # Check Message IDs
    id1 = msg1.status.message.message_id
    id2 = msg2.status.message.message_id
    id3 = msg3.status.message.message_id

    print(f"ID1: {id1}, ID2: {id2}, ID3: {id3}")

    assert id1 is not None
    assert id1 == id2, "Message ID should be reused for partial chunks"
    assert id1 == id3, "Message ID should be reused for final event too"
