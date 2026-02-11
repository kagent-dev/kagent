from unittest.mock import Mock

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.types import Message
from kagent.dapr._durable import _convert_a2a_request_to_span_attributes, _get_user_id


def _make_context(*, user_name: str | None = None, has_message: bool = True) -> RequestContext:
    ctx = Mock(spec=RequestContext)
    ctx.context_id = "ctx-123"
    ctx.task_id = "task-456"

    if has_message:
        ctx.message = Mock(spec=Message)
    else:
        ctx.message = None

    if user_name:
        call_context = Mock()
        call_context.user = Mock()
        call_context.user.user_name = user_name
        ctx.call_context = call_context
    else:
        ctx.call_context = None

    return ctx


def test_get_user_id_from_call_context():
    """user_name in call_context is returned."""
    ctx = _make_context(user_name="alice")
    assert _get_user_id(ctx) == "alice"


def test_get_user_id_fallback():
    """No call_context falls back to A2A_USER_{context_id}."""
    ctx = _make_context()
    assert _get_user_id(ctx) == "A2A_USER_ctx-123"


def test_convert_span_attributes():
    """All expected keys are present."""
    ctx = _make_context(user_name="bob")
    attrs = _convert_a2a_request_to_span_attributes(ctx)

    assert attrs["kagent.user_id"] == "bob"
    assert attrs["gen_ai.conversation.id"] == "ctx-123"
    assert attrs["gen_ai.task.id"] == "task-456"


def test_convert_span_attributes_no_task_id():
    """task_id absent means no gen_ai.task.id key."""
    ctx = _make_context()
    ctx.task_id = None
    attrs = _convert_a2a_request_to_span_attributes(ctx)

    assert "gen_ai.task.id" not in attrs
    assert "kagent.user_id" in attrs


def test_convert_span_attributes_no_message():
    """Raises ValueError when message is None."""
    ctx = _make_context(has_message=False)
    with pytest.raises(ValueError, match="cannot be None"):
        _convert_a2a_request_to_span_attributes(ctx)
