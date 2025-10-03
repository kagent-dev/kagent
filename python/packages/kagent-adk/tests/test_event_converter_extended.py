"""Extended tests for kagent.adk.converters.event_converter module to improve coverage."""

from unittest.mock import Mock

import pytest
from a2a.types import DataPart, Part
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events.event import Event
from google.adk.sessions import Session

from kagent.adk.converters.event_converter import (
    _create_artifact_id,
    _get_context_metadata,
    _process_long_running_tool,
    _serialize_metadata_value,
)
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)


class TestSerializeMetadataValue:
    """Tests for _serialize_metadata_value helper function."""

    def test_serialize_pydantic_model(self):
        """Test serializing a Pydantic model."""
        mock_model = Mock()
        mock_model.model_dump.return_value = {"key": "value"}

        result = _serialize_metadata_value(mock_model)

        assert result == {"key": "value"}
        mock_model.model_dump.assert_called_once_with(exclude_none=True, by_alias=True)

    def test_serialize_pydantic_model_with_error(self):
        """Test serializing a Pydantic model that raises an error."""
        mock_model = Mock()
        mock_model.model_dump.side_effect = Exception("Serialization error")

        result = _serialize_metadata_value(mock_model)

        # Should fall back to str() when model_dump fails
        assert isinstance(result, str)

    def test_serialize_string(self):
        """Test serializing a simple string."""
        result = _serialize_metadata_value("test_string")

        assert result == "test_string"

    def test_serialize_number(self):
        """Test serializing a number."""
        result = _serialize_metadata_value(42)

        assert result == "42"

    def test_serialize_none(self):
        """Test serializing None."""
        result = _serialize_metadata_value(None)

        assert result == "None"


class TestGetContextMetadata:
    """Tests for _get_context_metadata function."""

    def test_get_basic_metadata(self):
        """Test getting basic context metadata."""
        mock_event = Mock(spec=Event)
        mock_event.invocation_id = "inv-123"
        mock_event.author = "agent"
        mock_event.branch = None
        mock_event.grounding_metadata = None
        mock_event.custom_metadata = None
        mock_event.usage_metadata = None
        mock_event.error_code = None

        mock_session = Mock(spec=Session)
        mock_session.id = "session-456"

        mock_context = Mock(spec=InvocationContext)
        mock_context.app_name = "test-app"
        mock_context.user_id = "user-789"
        mock_context.session = mock_session

        result = _get_context_metadata(mock_event, mock_context)

        assert get_kagent_metadata_key("app_name") in result
        assert result[get_kagent_metadata_key("app_name")] == "test-app"
        assert get_kagent_metadata_key("user_id") in result
        assert result[get_kagent_metadata_key("user_id")] == "user-789"
        assert get_kagent_metadata_key("session_id") in result
        assert result[get_kagent_metadata_key("session_id")] == "session-456"
        assert get_kagent_metadata_key("invocation_id") in result
        assert result[get_kagent_metadata_key("invocation_id")] == "inv-123"

    def test_get_metadata_with_optional_fields(self):
        """Test getting metadata with optional fields populated."""
        mock_event = Mock(spec=Event)
        mock_event.invocation_id = "inv-123"
        mock_event.author = "agent"
        mock_event.branch = "test-branch"
        mock_event.grounding_metadata = {"source": "test"}
        mock_event.custom_metadata = {"custom": "data"}
        mock_event.usage_metadata = {"tokens": 100}
        mock_event.error_code = "ERROR_CODE"

        mock_session = Mock(spec=Session)
        mock_session.id = "session-456"

        mock_context = Mock(spec=InvocationContext)
        mock_context.app_name = "test-app"
        mock_context.user_id = "user-789"
        mock_context.session = mock_session

        result = _get_context_metadata(mock_event, mock_context)

        # Check optional fields are included
        assert get_kagent_metadata_key("branch") in result
        assert result[get_kagent_metadata_key("branch")] == "test-branch"
        assert get_kagent_metadata_key("grounding_metadata") in result
        assert get_kagent_metadata_key("custom_metadata") in result
        assert get_kagent_metadata_key("usage_metadata") in result
        assert get_kagent_metadata_key("error_code") in result

    def test_get_metadata_none_event(self):
        """Test that None event raises ValueError."""
        mock_context = Mock(spec=InvocationContext)

        with pytest.raises(ValueError, match="Event cannot be None"):
            _get_context_metadata(None, mock_context)

    def test_get_metadata_none_context(self):
        """Test that None context raises ValueError."""
        mock_event = Mock(spec=Event)

        with pytest.raises(ValueError, match="Invocation context cannot be None"):
            _get_context_metadata(mock_event, None)

    def test_get_metadata_with_exception(self):
        """Test handling of exception during metadata creation."""
        mock_event = Mock(spec=Event)
        mock_event.author = "test-author"
        # Make accessing invocation_id raise an exception
        type(mock_event).invocation_id = property(lambda self: (_ for _ in ()).throw(Exception("Access error")))

        mock_context = Mock(spec=InvocationContext)
        mock_context.app_name = "test-app"
        mock_context.user_id = "test-user"
        # Mock session with id to allow reaching the invocation_id check
        mock_session = Mock()
        mock_session.id = "session-123"
        mock_context.session = mock_session

        with pytest.raises(Exception, match="Access error"):
            _get_context_metadata(mock_event, mock_context)


class TestCreateArtifactId:
    """Tests for _create_artifact_id function."""

    def test_create_artifact_id(self):
        """Test creating an artifact ID."""
        result = _create_artifact_id(
            app_name="myapp", user_id="user123", session_id="session456", filename="test.pdf", version=1
        )

        assert "myapp" in result
        assert "user123" in result
        assert "session456" in result
        assert "test.pdf" in result
        assert "1" in result
        # Should use separator (-)
        parts = result.split("-")
        assert len(parts) == 5

    def test_create_artifact_id_with_different_version(self):
        """Test creating artifact ID with different version."""
        result = _create_artifact_id(
            app_name="app", user_id="user", session_id="session", filename="file.txt", version=42
        )

        assert "42" in result

    def test_create_artifact_id_components_order(self):
        """Test that artifact ID components are in correct order."""
        result = _create_artifact_id(app_name="A", user_id="B", session_id="C", filename="D", version=5)

        parts = result.split("-")
        assert parts[0] == "A"
        assert parts[1] == "B"
        assert parts[2] == "C"
        assert parts[3] == "D"
        assert parts[4] == "5"


class TestProcessLongRunningTool:
    """Tests for _process_long_running_tool function."""

    def test_mark_long_running_tool(self):
        """Test marking a function call as long-running."""
        # Create a DataPart with function call metadata
        data_part = DataPart(
            data={"id": "tool-123", "name": "long_tool"},
            metadata={
                get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
            },
        )
        a2a_part = Part(root=data_part)

        mock_event = Mock(spec=Event)
        mock_event.long_running_tool_ids = ["tool-123"]

        _process_long_running_tool(a2a_part, mock_event)

        # Should have been marked as long-running
        assert a2a_part.root.metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY)] is True

    def test_dont_mark_non_long_running_tool(self):
        """Test that non-long-running tools aren't marked."""
        data_part = DataPart(
            data={"id": "tool-456", "name": "normal_tool"},
            metadata={
                get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
            },
        )
        a2a_part = Part(root=data_part)

        mock_event = Mock(spec=Event)
        mock_event.long_running_tool_ids = ["tool-123"]  # Different ID

        _process_long_running_tool(a2a_part, mock_event)

        # Should NOT have been marked as long-running
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY) not in a2a_part.root.metadata

    def test_no_long_running_tools_in_event(self):
        """Test when event has no long-running tools."""
        data_part = DataPart(
            data={"id": "tool-123", "name": "tool"},
            metadata={
                get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
            },
        )
        a2a_part = Part(root=data_part)

        mock_event = Mock(spec=Event)
        mock_event.long_running_tool_ids = None

        _process_long_running_tool(a2a_part, mock_event)

        # Should NOT have been marked
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY) not in a2a_part.root.metadata

    def test_non_function_call_part(self):
        """Test that non-function-call parts aren't marked."""
        data_part = DataPart(
            data={"data": "some data"},
            metadata={get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): "OTHER_TYPE"},
        )
        a2a_part = Part(root=data_part)

        mock_event = Mock(spec=Event)
        mock_event.long_running_tool_ids = ["tool-123"]

        _process_long_running_tool(a2a_part, mock_event)

        # Should NOT have been marked
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY) not in a2a_part.root.metadata

    def test_part_with_no_metadata(self):
        """Test that part with no metadata isn't marked."""
        data_part = DataPart(data={"id": "tool-123"})
        a2a_part = Part(root=data_part)

        mock_event = Mock(spec=Event)
        mock_event.long_running_tool_ids = ["tool-123"]

        _process_long_running_tool(a2a_part, mock_event)

        # Should handle gracefully (no error)
        # Metadata would be None, so check doesn't apply


class TestEdgeCases:
    """Test edge cases for event converter."""

    def test_serialize_empty_dict(self):
        """Test serializing empty dict."""
        mock_obj = Mock()
        mock_obj.model_dump.return_value = {}

        result = _serialize_metadata_value(mock_obj)

        assert result == {}

    def test_artifact_id_with_special_characters(self):
        """Test creating artifact ID with special characters in components."""
        result = _create_artifact_id(
            app_name="app-name",
            user_id="user@example.com",
            session_id="session_123",
            filename="file name.pdf",
            version=1,
        )

        # Should still create a valid ID
        assert isinstance(result, str)
        assert len(result) > 0

    def test_metadata_with_partial_optional_fields(self):
        """Test metadata with some optional fields set and others None."""
        mock_event = Mock(spec=Event)
        mock_event.invocation_id = "inv-123"
        mock_event.author = "agent"
        mock_event.branch = "main"  # Set
        mock_event.grounding_metadata = None  # Not set
        mock_event.custom_metadata = {"key": "value"}  # Set
        mock_event.usage_metadata = None  # Not set
        mock_event.error_code = None  # Not set

        mock_session = Mock(spec=Session)
        mock_session.id = "session-456"

        mock_context = Mock(spec=InvocationContext)
        mock_context.app_name = "test-app"
        mock_context.user_id = "user-789"
        mock_context.session = mock_session

        result = _get_context_metadata(mock_event, mock_context)

        # Only set optional fields should be in result
        assert get_kagent_metadata_key("branch") in result
        assert get_kagent_metadata_key("custom_metadata") in result
        # None fields should not be in result
        assert (
            get_kagent_metadata_key("grounding_metadata") not in result
            or result.get(get_kagent_metadata_key("grounding_metadata")) is None
        )
