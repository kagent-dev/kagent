from __future__ import annotations

from unittest.mock import AsyncMock

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.context import ServerCallContext
from a2a.types import Message, Part, Role, SendMessageRequest
from google.adk.a2a.converters.request_converter import AgentRunRequest
from google.adk.agents.run_config import RunConfig, StreamingMode

import kagent.adk._agent_executor as executor_module
from kagent.adk._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig
from kagent.adk._bearer_token import bearer_token


@pytest.fixture(autouse=True)
def _reset_bearer_token():
    token = bearer_token.set(None)
    yield
    bearer_token.reset(token)


def _request_context(*, state: dict | None = None) -> RequestContext:
    message = Message(
        message_id="message-1",
        role=Role.ROLE_USER,
        parts=[Part(text="hello from a2a")],
    )
    return RequestContext(
        ServerCallContext(state=state or {}),
        SendMessageRequest(message=message),
        task_id="task-1",
        context_id="context-1",
    )


@pytest.mark.parametrize(
    ("stream", "expected_mode"),
    [(False, StreamingMode.NONE), (True, StreamingMode.SSE)],
)
def test_request_converter_adds_headers_without_mutating_message_metadata(stream, expected_mode):
    context = _request_context(state={"headers": {"authorization": "Bearer token"}})
    executor = A2aAgentExecutor(runner=lambda: None, config=A2aAgentExecutorConfig(stream=stream))

    run_request = executor._convert_request(context, None)

    assert run_request.run_config.streaming_mode == expected_mode
    assert run_request.state_delta == {"headers": {"authorization": "Bearer token"}}
    assert not context.message.metadata


def test_convert_request_sets_bearer_token_context_var():
    context = _request_context(state={"headers": {"authorization": "Bearer the-callers-token"}})
    executor = A2aAgentExecutor(runner=lambda: None, config=A2aAgentExecutorConfig())

    executor._convert_request(context, None)

    assert bearer_token.get() == "the-callers-token"


def test_convert_request_clears_bearer_token_when_no_auth_header():
    context = _request_context(state={"headers": {}})
    executor = A2aAgentExecutor(runner=lambda: None, config=A2aAgentExecutorConfig())

    executor._convert_request(context, None)

    assert bearer_token.get() is None


@pytest.mark.asyncio
async def test_execute_delegates_to_adk_2_executor_and_closes_request_runner(monkeypatch):
    context = _request_context()
    event_queue = object()
    runner = object()
    run_request = AgentRunRequest(
        user_id="user-1",
        session_id="context-1",
        run_config=RunConfig(),
    )
    executor = A2aAgentExecutor(runner=lambda: None)
    executor._resolve_runner = AsyncMock(return_value=runner)
    executor._convert_request = lambda request_context, part_converter: run_request
    executor._prepare_session = AsyncMock()
    executor._safe_close_runner = AsyncMock()

    calls = {}

    class FakeUpstreamExecutor:
        def __init__(self, *, runner, config, force_new_version):
            calls.update(runner=runner, config=config, force_new_version=force_new_version)

        async def execute(self, request_context, queue):
            calls.update(context=request_context, event_queue=queue)

    monkeypatch.setattr(executor_module, "UpstreamA2aAgentExecutor", FakeUpstreamExecutor)

    await executor.execute(context, event_queue)

    assert calls["runner"] is runner
    assert calls["force_new_version"] is True
    assert calls["context"] is context
    assert calls["event_queue"] is event_queue
    assert calls["config"].request_converter == executor._convert_request
    executor._prepare_session.assert_awaited_once_with(context, run_request, runner)
    executor._safe_close_runner.assert_awaited_once_with(runner)
