from unittest.mock import Mock

import pytest
from a2a.server.agent_execution import RequestContext
from google.adk.runners import RunConfig
from google.adk.agents.run_config import StreamingMode

from kagent.adk.converters.request_converter import convert_a2a_request_to_adk_run_args


def _create_mock_request_context(context_id="test_session"):
    """Create a mock request context for testing."""
    context = Mock(spec=RequestContext)
    context.context_id = context_id
    context.message = Mock()
    context.message.parts = []  # Empty parts for simplicity
    context.call_context = Mock()
    context.call_context.user = Mock()
    context.call_context.user.user_name = "test_user"
    return context


class TestRequestConverter:
    """Test cases for request converter functions."""

    def test_convert_request_streaming_modes(self):
        """Test that the stream parameter correctly maps to StreamingMode."""
        request = _create_mock_request_context()

        # Test case 1: Stream = False (default)
        result_default = convert_a2a_request_to_adk_run_args(request, stream=False)
        assert isinstance(result_default["run_config"], RunConfig)
        assert result_default["run_config"].streaming_mode == StreamingMode.NONE

        # Test case 2: Stream = True
        result_stream = convert_a2a_request_to_adk_run_args(request, stream=True)
        assert isinstance(result_stream["run_config"], RunConfig)
        assert result_stream["run_config"].streaming_mode == StreamingMode.SSE

    def test_convert_request_basic_fields(self):
        """Test that basic fields are correctly mapped."""
        request = _create_mock_request_context(context_id="my_session_123")

        result = convert_a2a_request_to_adk_run_args(request)

        assert result["user_id"] == "test_user"
        assert result["session_id"] == "my_session_123"
        assert result["new_message"].role == "user"
