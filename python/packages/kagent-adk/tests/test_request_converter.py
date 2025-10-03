"""Tests for kagent.adk.converters.request_converter module."""

from unittest.mock import Mock

import pytest
from a2a import types as a2a_types
from a2a.server.agent_execution import RequestContext

from kagent.adk.converters.request_converter import (
    _get_user_id,
    convert_a2a_request_to_adk_run_args,
)


class TestGetUserId:
    """Tests for _get_user_id helper function."""

    def test_get_user_from_call_context(self):
        """Test getting user ID from call context."""
        request = Mock(spec=RequestContext)
        request.call_context = Mock()
        request.call_context.user = Mock()
        request.call_context.user.user_name = "john_doe"
        request.context_id = "context_123"

        user_id = _get_user_id(request)

        assert user_id == "john_doe"

    def test_get_user_from_context_id_no_call_context(self):
        """Test getting user ID from context_id when no call context."""
        request = Mock(spec=RequestContext)
        request.call_context = None
        request.context_id = "context_456"

        user_id = _get_user_id(request)

        assert user_id == "A2A_USER_context_456"

    def test_get_user_from_context_id_no_user(self):
        """Test getting user ID from context_id when no user in context."""
        request = Mock(spec=RequestContext)
        request.call_context = Mock()
        request.call_context.user = None
        request.context_id = "context_789"

        user_id = _get_user_id(request)

        assert user_id == "A2A_USER_context_789"

    def test_get_user_from_context_id_empty_user_name(self):
        """Test getting user ID from context_id when user_name is empty."""
        request = Mock(spec=RequestContext)
        request.call_context = Mock()
        request.call_context.user = Mock()
        request.call_context.user.user_name = ""
        request.context_id = "context_abc"

        user_id = _get_user_id(request)

        assert user_id == "A2A_USER_context_abc"


class TestConvertA2ARequestToADKRunArgs:
    """Tests for convert_a2a_request_to_adk_run_args function."""

    def test_convert_basic_request(self):
        """Test converting a basic A2A request to ADK run args."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_123"
        request.call_context = None
        request.message = Mock()
        request.message.parts = [a2a_types.Part(root=a2a_types.TextPart(text="Hello"))]

        result = convert_a2a_request_to_adk_run_args(request)

        assert result is not None
        assert result["user_id"] == "A2A_USER_session_123"
        assert result["session_id"] == "session_123"
        assert result["new_message"].role == "user"
        assert len(result["new_message"].parts) == 1
        assert result["run_config"] is not None

    def test_convert_request_with_authenticated_user(self):
        """Test converting request with authenticated user."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_456"
        request.call_context = Mock()
        request.call_context.user = Mock()
        request.call_context.user.user_name = "authenticated_user"
        request.message = Mock()
        request.message.parts = [a2a_types.Part(root=a2a_types.TextPart(text="Authenticated message"))]

        result = convert_a2a_request_to_adk_run_args(request)

        assert result["user_id"] == "authenticated_user"
        assert result["session_id"] == "session_456"

    def test_convert_request_with_multiple_parts(self):
        """Test converting request with multiple message parts."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_789"
        request.call_context = None
        request.message = Mock()
        request.message.parts = [
            a2a_types.Part(root=a2a_types.TextPart(text="Part 1")),
            a2a_types.Part(root=a2a_types.TextPart(text="Part 2")),
            a2a_types.Part(root=a2a_types.TextPart(text="Part 3")),
        ]

        result = convert_a2a_request_to_adk_run_args(request)

        assert len(result["new_message"].parts) == 3

    def test_convert_request_none_message_raises_error(self):
        """Test that None message raises ValueError."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_error"
        request.call_context = None
        request.message = None

        with pytest.raises(ValueError, match="Request message cannot be None"):
            convert_a2a_request_to_adk_run_args(request)

    def test_convert_request_with_file_part(self):
        """Test converting request with file part."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_file"
        request.call_context = None
        request.message = Mock()
        request.message.parts = [
            a2a_types.Part(
                root=a2a_types.FilePart(
                    file=a2a_types.FileWithUri(uri="https://example.com/file.pdf", mime_type="application/pdf")
                )
            )
        ]

        result = convert_a2a_request_to_adk_run_args(request)

        assert result is not None
        assert len(result["new_message"].parts) == 1
        # The part converter should have converted the file part
        assert result["new_message"].parts[0] is not None

    def test_run_config_always_created(self):
        """Test that run_config is always included in result."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_config"
        request.call_context = None
        request.message = Mock()
        request.message.parts = [a2a_types.Part(root=a2a_types.TextPart(text="Test"))]

        result = convert_a2a_request_to_adk_run_args(request)

        assert "run_config" in result
        assert result["run_config"] is not None

    def test_user_role_in_content(self):
        """Test that converted content always has 'user' role."""
        request = Mock(spec=RequestContext)
        request.context_id = "session_role"
        request.call_context = None
        request.message = Mock()
        request.message.parts = [a2a_types.Part(root=a2a_types.TextPart(text="User message"))]

        result = convert_a2a_request_to_adk_run_args(request)

        assert result["new_message"].role == "user"
