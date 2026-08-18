from enum import Enum
from unittest.mock import Mock

import pytest
from a2a.types import TaskArtifactUpdateEvent, TaskState, TaskStatusUpdateEvent
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import get_kagent_metadata_key
from pydantic import BaseModel, Field

from kagent.adk.converters.event_converter import convert_event_to_a2a_events, serialize_metadata_value


def _create_mock_invocation_context():
    """Create a mock invocation context for testing."""
    context = Mock()
    context.app_name = "test_app"
    context.user_id = "test_user"
    context.session.id = "test_session"
    return context


def _create_mock_event(
    error_code=None, content=None, invocation_id="test_invocation", author="test_author", partial=False
):
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
    event.partial = partial
    event.long_running_tool_ids = None
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
            e for e in result1 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_FAILED
        ]
        working_events1 = [
            e
            for e in result1
            if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_WORKING
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
            e for e in result2 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_FAILED
        ]
        working_events2 = [
            e
            for e in result2
            if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_WORKING
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
            e for e in result3 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_FAILED
        ]
        working_events3 = [
            e
            for e in result3
            if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_WORKING
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
            e for e in result4 if isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_FAILED
        ]
        assert len(error_events4) == 1, f"Expected 1 error event for MALFORMED_FUNCTION_CALL, got {len(error_events4)}"

        # Check that the error event has the correct error code in metadata
        error_event = error_events4[0]
        error_code_key = get_kagent_metadata_key("error_code")
        assert error_code_key in error_event.metadata
        assert error_event.metadata[error_code_key] == str(genai_types.FinishReason.MALFORMED_FUNCTION_CALL)

    def test_content_is_emitted_as_artifact(self):
        invocation_context = _create_mock_invocation_context()
        content = genai_types.Content(parts=[genai_types.Part(text="hello world")])
        event = _create_mock_event(content=content, invocation_id="test_invocation_ids")

        result = convert_event_to_a2a_events(event, invocation_context, task_id="task-xyz", context_id="ctx-xyz")

        artifact_events = [e for e in result if isinstance(e, TaskArtifactUpdateEvent)]
        assert len(artifact_events) == 1
        artifact_event = artifact_events[0]
        assert artifact_event.task_id == "task-xyz"
        assert artifact_event.context_id == "ctx-xyz"
        assert artifact_event.artifact.parts[0].text == "hello world"
        assert artifact_event.last_chunk is True
        assert get_kagent_metadata_key("adk_partial") not in artifact_event.metadata
        assert not any(
            isinstance(e, TaskStatusUpdateEvent) and e.status.state == TaskState.TASK_STATE_WORKING for e in result
        )

    def test_partial_chunks_reuse_artifact_id_and_final_replaces(self):
        """Go OutputArtifactPerEvent framing: append deltas, replace+close on final."""
        invocation_context = _create_mock_invocation_context()
        agents_artifacts: dict[str, str] = {}

        first = convert_event_to_a2a_events(
            _create_mock_event(content=genai_types.Content(parts=[genai_types.Part(text="hel")]), partial=True),
            invocation_context,
            agents_artifacts=agents_artifacts,
        )[0]
        second = convert_event_to_a2a_events(
            _create_mock_event(content=genai_types.Content(parts=[genai_types.Part(text="lo")]), partial=True),
            invocation_context,
            agents_artifacts=agents_artifacts,
        )[0]
        final = convert_event_to_a2a_events(
            _create_mock_event(content=genai_types.Content(parts=[genai_types.Part(text="hello")]), partial=False),
            invocation_context,
            agents_artifacts=agents_artifacts,
        )[0]

        assert isinstance(first, TaskArtifactUpdateEvent)
        assert first.artifact.artifact_id == second.artifact.artifact_id == final.artifact.artifact_id
        assert first.append is False
        assert first.last_chunk is False
        assert second.append is True
        assert second.last_chunk is False
        assert final.append is False
        assert final.last_chunk is True
        assert final.artifact.parts[0].text == "hello"
        assert agents_artifacts == {}

    def test_final_mixed_event_keeps_text_and_hitl_parts_on_same_artifact(self):
        invocation_context = _create_mock_invocation_context()
        agents_artifacts: dict[str, str] = {}
        partial_event = _create_mock_event(
            content=genai_types.Content(parts=[genai_types.Part(text="partial text")]), partial=True
        )
        partial_artifact = convert_event_to_a2a_events(
            partial_event, invocation_context, agents_artifacts=agents_artifacts
        )[0]

        final_event = _create_mock_event(
            content=genai_types.Content(
                parts=[
                    genai_types.Part(text="partial text complete"),
                    genai_types.Part(
                        function_call=genai_types.FunctionCall(id="call-1", name="dangerous_tool", args={"value": "x"})
                    ),
                ]
            ),
            partial=False,
        )
        final_event.long_running_tool_ids = {"call-1"}

        result = convert_event_to_a2a_events(final_event, invocation_context, agents_artifacts=agents_artifacts)

        assert len(result) == 1
        final_artifact = result[0]
        assert isinstance(final_artifact, TaskArtifactUpdateEvent)
        assert final_artifact.last_chunk is True
        assert final_artifact.append is False
        assert final_artifact.artifact.artifact_id == partial_artifact.artifact.artifact_id
        assert final_artifact.artifact.parts[0].text == "partial text complete"
        assert MessageToDict(final_artifact.artifact.parts[1].data)["name"] == "dangerous_tool"
        assert agents_artifacts == {}


class TestSerializeMetadataValue:
    """Test cases for serialize_metadata_value."""

    def test_pydantic_value_serializes_to_dump_dict(self):
        """A Pydantic value serializes to its JSON-compatible model_dump dict so
        structured metadata (e.g. usage_metadata token counts) stays
        machine-readable for consumers such as the UI."""

        class _Status(Enum):
            READY = "ready"

        class _Model(BaseModel):
            a: int
            b: str | None = None
            status: _Status
            alias_value: str = Field(alias="aliasValue")

        value = _Model(a=1, status=_Status.READY, aliasValue="x")

        result = serialize_metadata_value(value)

        assert result == {"a": 1, "status": "ready", "aliasValue": "x"}

    def test_plain_value_serializes_to_str(self):
        assert serialize_metadata_value(42) == "42"
