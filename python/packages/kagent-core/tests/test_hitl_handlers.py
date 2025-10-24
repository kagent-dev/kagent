"""Tests for HITL handler functions."""

from unittest.mock import AsyncMock, Mock

import pytest
from a2a.server.events.event_queue import EventQueue
from a2a.server.tasks import TaskStore
from a2a.types import TaskState, TaskStatusUpdateEvent

from kagent.core.a2a import (
    KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL,
    ToolApprovalRequest,
    handle_tool_approval_interrupt,
)


@pytest.mark.asyncio
async def test_handle_tool_approval_interrupt():
    """Test tool approval interrupt handling with single and multiple actions."""
    # Setup mocks
    event_queue = Mock(spec=EventQueue)
    event_queue.enqueue_event = AsyncMock()

    task_store = Mock(spec=TaskStore)
    task_store.wait_for_save = AsyncMock()

    # Test single action
    action_requests = [ToolApprovalRequest(name="search", args={"query": "test"})]

    await handle_tool_approval_interrupt(
        action_requests=action_requests,
        task_id="task123",
        context_id="ctx456",
        event_queue=event_queue,
        task_store=task_store,
        app_name="test_app",
    )

    # Verify event was enqueued
    assert event_queue.enqueue_event.call_count == 1
    event = event_queue.enqueue_event.call_args[0][0]
    assert isinstance(event, TaskStatusUpdateEvent)
    assert event.task_id == "task123"
    assert event.context_id == "ctx456"
    assert event.status.state == TaskState.input_required
    assert event.final is False
    assert event.metadata["interrupt_type"] == KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL
    task_store.wait_for_save.assert_called_once_with("task123", timeout=5.0)

    # Reset mocks
    event_queue.enqueue_event.reset_mock()
    task_store.wait_for_save.reset_mock()

    # Test multiple actions
    action_requests = [
        ToolApprovalRequest(name="tool1", args={"a": 1}),
        ToolApprovalRequest(name="tool2", args={"b": 2}),
    ]

    await handle_tool_approval_interrupt(
        action_requests=action_requests,
        task_id="task456",
        context_id="ctx789",
        event_queue=event_queue,
        task_store=task_store,
    )

    # Verify event contains all actions
    event = event_queue.enqueue_event.call_args[0][0]
    message = event.status.message
    assert len(message.parts) > 0

    # Find DataPart with action_requests
    data_parts = [p for p in message.parts if hasattr(p, "root") and hasattr(p.root, "data")]
    assert len(data_parts) > 0


@pytest.mark.asyncio
async def test_handle_tool_approval_interrupt_timeout():
    """Test that save timeout is handled gracefully."""
    event_queue = Mock(spec=EventQueue)
    event_queue.enqueue_event = AsyncMock()

    task_store = Mock(spec=TaskStore)
    # Simulate timeout
    task_store.wait_for_save = AsyncMock(side_effect=TimeoutError())

    action_requests = [ToolApprovalRequest(name="test", args={})]

    # Should not raise - timeout is caught and logged
    await handle_tool_approval_interrupt(
        action_requests=action_requests,
        task_id="task123",
        context_id="ctx456",
        event_queue=event_queue,
        task_store=task_store,
    )

    # Event should still be sent even if save times out
    event_queue.enqueue_event.assert_called_once()
