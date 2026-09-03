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


def test_span_attributes_skip_decode_when_allowlist_empty(monkeypatch):
    from kagent.core.tracing._context_attributes import TRACE_CONTEXT_KEYS_ENV_VAR, _allowed_context_mappings

    monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
    _allowed_context_mappings.cache_clear()
    decode = MagicMock(return_value={"thread_id": "T1"})
    monkeypatch.setattr("kagent.core.tracing._context_attributes.read_message_metadata", decode)
    import kagent.openai._agent_executor as executor_module

    if hasattr(executor_module, "read_message_metadata"):
        monkeypatch.setattr(executor_module, "read_message_metadata", decode)

    attrs = _convert_a2a_request_to_span_attributes(_request_context())

    decode.assert_not_called()
    assert "kagent.context.thread_id" not in attrs
    assert "kagent.user_id" in attrs


def test_span_attributes_do_not_override_runtime_user_id(monkeypatch):
    fake = MagicMock(return_value={"kagent.user_id": "attacker", "kagent.context.thread_id": "T1"})
    monkeypatch.setattr("kagent.core.tracing._context_attributes.caller_context_attributes", fake)
    import kagent.openai._agent_executor as executor_module

    if hasattr(executor_module, "caller_context_attributes"):
        monkeypatch.setattr(executor_module, "caller_context_attributes", fake)

    attrs = _convert_a2a_request_to_span_attributes(_request_context())

    assert attrs["kagent.user_id"] != "attacker"
    assert attrs["kagent.context.thread_id"] == "T1"
