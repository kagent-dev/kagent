"""Tests for kagent.adk.converters.request_converter module."""

from unittest.mock import Mock

import pytest
from a2a import types as a2a_types
from a2a.server.agent_execution import RequestContext
from a2a.server.context import ServerCallContext

from kagent.adk.converters.request_converter import convert_a2a_request_to_adk_run_args
from kagent.core.a2a import USER_ID_HEADER, USER_ID_PREFIX, create_user_from_header, extract_header, extract_user_id


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


class TestRequestUtilityFunctions:
    """Tests for request utility functions."""

    def test_extract_user_id_from_call_context(self):
        """Test extracting user ID from call context."""
        request = Mock(spec=RequestContext)
        request.call_context = Mock()
        request.call_context.user = Mock()
        request.call_context.user.user_name = "test_user"
        request.context_id = "ctx_123"

        user_id = extract_user_id(request)

        assert user_id == "test_user"

    def test_extract_user_id_fallback_to_context_id(self):
        """Test user ID falls back to context_id when no user in context."""
        request = Mock(spec=RequestContext)
        request.call_context = None
        request.context_id = "ctx_456"

        user_id = extract_user_id(request)

        assert user_id == "A2A_USER_ctx_456"

    def test_extract_user_id_with_empty_user_name(self):
        """Test user ID falls back when user_name is empty."""
        request = Mock(spec=RequestContext)
        request.call_context = Mock()
        request.call_context.user = Mock()
        request.call_context.user.user_name = ""
        request.context_id = "ctx_789"

        user_id = extract_user_id(request)

        assert user_id == "A2A_USER_ctx_789"

    def test_extract_header_basic(self):
        """Test extracting a header from ServerCallContext."""
        context = Mock(spec=ServerCallContext)
        context.state = {"headers": {"X-Custom-Header": "custom_value"}}

        result = extract_header(context, "X-Custom-Header")

        assert result == "custom_value"

    def test_extract_header_not_found_returns_default(self):
        """Test extracting non-existent header returns default."""
        context = Mock(spec=ServerCallContext)
        context.state = {"headers": {}}

        result = extract_header(context, "X-Missing-Header", default="default_value")

        assert result == "default_value"

    def test_extract_header_none_context_returns_default(self):
        """Test extracting header from None context returns default."""
        result = extract_header(None, "X-Any-Header", default="fallback")

        assert result == "fallback"

    def test_extract_header_no_headers_in_state(self):
        """Test extracting header when no headers in state."""
        context = Mock(spec=ServerCallContext)
        context.state = {}

        result = extract_header(context, "X-Header")

        assert result is None

    def test_create_user_from_header_success(self):
        """Test creating KAgentUser from header."""
        context = Mock(spec=ServerCallContext)
        context.state = {"headers": {"X-User-ID": "user_123"}}

        user = create_user_from_header(context)

        assert user is not None
        assert user.user_name == "user_123"
        assert user.is_authenticated is False

    def test_create_user_from_header_custom_header_name(self):
        """Test creating user from custom header name."""
        context = Mock(spec=ServerCallContext)
        context.state = {"headers": {"X-Custom-User": "custom_user_456"}}

        user = create_user_from_header(context, header_name="X-Custom-User")

        assert user is not None
        assert user.user_name == "custom_user_456"

    def test_create_user_from_header_not_found_returns_none(self):
        """Test creating user when header not found returns None."""
        context = Mock(spec=ServerCallContext)
        context.state = {"headers": {}}

        user = create_user_from_header(context)

        assert user is None

    def test_create_user_from_header_none_context_returns_none(self):
        """Test creating user from None context returns None."""
        user = create_user_from_header(None)

        assert user is None

    def test_user_id_header_constant(self):
        """Test USER_ID_HEADER constant is set correctly."""
        assert USER_ID_HEADER == "X-User-ID"

    def test_user_id_prefix_constant(self):
        """Test USER_ID_PREFIX constant is set correctly."""
        assert USER_ID_PREFIX == "A2A_USER_"
