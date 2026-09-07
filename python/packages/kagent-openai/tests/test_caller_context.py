from unittest.mock import MagicMock

from a2a.server.agent_execution.context import RequestContext
from a2a.types import Message, Part, Role, SendMessageRequest

from kagent.openai._agent_executor import _convert_a2a_request_to_span_attributes


def _request_context() -> RequestContext:
    message = Message(role=Role.ROLE_USER, message_id="msg-1", parts=[Part(text="hello")])
    return RequestContext(
        call_context=MagicMock(),
        request=SendMessageRequest(message=message),
        task_id="task-1",
        context_id="ctx-1",
    )


def test_span_attributes_are_runtime_only():
    attrs = _convert_a2a_request_to_span_attributes(_request_context())

    assert "kagent.user_id" in attrs
    assert attrs["gen_ai.conversation.id"] == "ctx-1"
    assert "kagent.context.thread_id" not in attrs
    assert "user.id" not in attrs
