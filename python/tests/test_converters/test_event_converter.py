import pytest
from unittest.mock import Mock
from a2a.types import TaskStatusUpdateEvent, TaskState
from google.genai import types as genai_types

from kagent.adk.converters.event_converter import convert_event_to_a2a_events
from kagent.core.a2a import get_kagent_metadata_key


def _create_mock_invocation_context():
    """Create a mock invocation context for testing."""
    context = Mock()
    context.app_name = "test_app"
    context.user_id = "test_user"
    context.session.id = "test_session"
    return context


def _create_mock_event(error_code=None, content=None, invocation_id="test_invocation", author="test_author"):
    """Create a mock event for testing."""
    event = Mock()
    event.error_code = error_code
    event.content = content
    event.invocation_id = invocation_id
    event.author = author
    event.branch = None
    event.grounding_metadata = None
    event.custom_metadata = None
    event.usage_metadata = None
    event.error_message = None
    return event


class TestEventConverter:
    """Test cases for event converter functions."""

    def test_convert_event_to_a2a_events(self):
        """Test that STOP error codes with empty content don't create any events, while actual error codes create error events."""

        invocation_context = _create_mock_invocation_context()

        # Test case 1: Empty content with STOP error code
        event1 = _create_mock_event(
            error_code=genai_types.FinishReason.STOP, content=None, invocation_id="test_invocation_1"
        )
        result1 = convert_event_to_a2a_events(
            event1, invocation_context, task_id="test_task_1", context_id="test_context_1"
        )
        error_events1 = [
            e for e in result1 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.failed
        ]
        working_events1 = [
            e for e in result1 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.working
        ]
        assert len(error_events1) == 0, (
            f"Expected no error events for STOP with empty content, got {len(error_events1)}"
        )
        assert len(working_events1) == 0, (
            f"Expected no working events for STOP with empty content (no content to convert), got {len(working_events1)}"
        )

        # Test case 2: Empty parts with STOP error code
        content_mock = Mock()
        content_mock.parts = []
        event2 = _create_mock_event(
            error_code=genai_types.FinishReason.STOP, content=content_mock, invocation_id="test_invocation_2"
        )
        result2 = convert_event_to_a2a_events(
            event2, invocation_context, task_id="test_task_2", context_id="test_context_2"
        )
        error_events2 = [
            e for e in result2 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.failed
        ]
        working_events2 = [
            e for e in result2 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.working
        ]
        assert len(error_events2) == 0, f"Expected no error events for STOP with empty parts, got {len(error_events2)}"
        assert len(working_events2) == 0, (
            f"Expected no working events for STOP with empty parts (no content to convert), got {len(working_events2)}"
        )

        # Test case 3: Missing content with STOP error code
        event3 = _create_mock_event(
            error_code=genai_types.FinishReason.STOP, content=None, invocation_id="test_invocation_3"
        )
        result3 = convert_event_to_a2a_events(
            event3, invocation_context, task_id="test_task_3", context_id="test_context_3"
        )
        error_events3 = [
            e for e in result3 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.failed
        ]
        working_events3 = [
            e for e in result3 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.working
        ]
        assert len(error_events3) == 0, (
            f"Expected no error events for STOP with missing content, got {len(error_events3)}"
        )
        assert len(working_events3) == 0, (
            f"Expected no working events for STOP with missing content (no content to convert), got {len(working_events3)}"
        )

        # Test case 4: Actual error code should create error event
        event4 = _create_mock_event(
            error_code=genai_types.FinishReason.MALFORMED_FUNCTION_CALL, content=None, invocation_id="test_invocation_4"
        )
        result4 = convert_event_to_a2a_events(
            event4, invocation_context, task_id="test_task_4", context_id="test_context_4"
        )
        error_events4 = [
            e for e in result4 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.failed
        ]
        assert len(error_events4) == 1, f"Expected 1 error event for MALFORMED_FUNCTION_CALL, got {len(error_events4)}"

        # Check that the error event has the correct error code in metadata
        error_event = error_events4[0]
        error_code_key = get_kagent_metadata_key("error_code")
        assert error_code_key in error_event.metadata
        assert error_event.metadata[error_code_key] == str(genai_types.FinishReason.MALFORMED_FUNCTION_CALL)
