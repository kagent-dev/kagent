from unittest.mock import AsyncMock, MagicMock

import pytest
from google.protobuf.json_format import ParseDict
from opentelemetry import baggage
from opentelemetry import context as otel_context
from kagent.core.tracing._context_attributes import _allowed_context_mappings
from kagent.core.tracing._span_processor import KAGENT_ATTRIBUTES_KEY

from a2a.server.agent_execution.context import RequestContext
from a2a.types import Message, Part, Role, SendMessageRequest

from kagent.openai._agent_executor import OpenAIAgentExecutor, _convert_a2a_request_to_span_attributes


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


@pytest.mark.asyncio
@pytest.mark.parametrize("fails", [False, True])
async def test_execute_promotes_metadata_and_restores_context(monkeypatch, fails):
    monkeypatch.setenv("KAGENT_TRACE_CONTEXT_KEYS", "thread_id")
    _allowed_context_mappings.cache_clear()
    context = _request_context()
    ParseDict({"thread_id": "T1", "private_token": "must-not-be-exported"}, context.message.metadata)
    executor = OpenAIAgentExecutor(agent=MagicMock(), app_name="test")
    original_context = otel_context.get_current()

    async def run_agent(request, queue):
        assert baggage.get_baggage("thread_id") == "T1"
        assert baggage.get_baggage("private_token") is None
        assert otel_context.get_value(KAGENT_ATTRIBUTES_KEY)["gen_ai.conversation.id"] == "ctx-1"
        if fails:
            raise RuntimeError("test failure")

    executor._execute_agent = AsyncMock(side_effect=run_agent)
    try:
        if fails:
            with pytest.raises(RuntimeError, match="test failure"):
                await executor.execute(context, MagicMock())
        else:
            await executor.execute(context, MagicMock())
        executor._execute_agent.assert_awaited_once()
        assert otel_context.get_current() is original_context
    finally:
        _allowed_context_mappings.cache_clear()
