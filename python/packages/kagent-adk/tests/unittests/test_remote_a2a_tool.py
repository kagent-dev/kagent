"""Tests for KAgentRemoteA2ATool."""

from typing import Any, AsyncIterator
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
from a2a.types import (
    DataPart,
    Role,
    Task,
    TaskState,
    TaskStatus,
    TextPart,
)
from a2a.types import Message as A2AMessage
from a2a.types import Part as A2APart
from google.adk.tools.tool_confirmation import ToolConfirmation
from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
)

from kagent.adk._remote_a2a_tool import (
    KAgentRemoteA2ATool,
    KAgentRemoteA2AToolset,
    SubagentSessionProvider,
    _SubagentInterceptor,
)

# ---------------------------------------------------------------------------
# Test helpers
# ---------------------------------------------------------------------------

_DEFAULT_USER_ID = "admin@kagent.dev"


class _MockSession:
    """Minimal session mock providing user_id."""

    def __init__(self, user_id: str = _DEFAULT_USER_ID):
        self.user_id = user_id


class MockToolContext:
    """Minimal ToolContext mock matching the interface used by KAgentRemoteA2ATool."""

    def __init__(
        self,
        tool_confirmation: ToolConfirmation | None = None,
        user_id: str = _DEFAULT_USER_ID,
    ):
        self.state: dict[str, Any] = {}
        self.function_call_id = "outer_fc_1"
        self.tool_confirmation = tool_confirmation
        self.session = _MockSession(user_id)
        self._confirmations: dict[str, ToolConfirmation] = {}

    def request_confirmation(self, *, hint: str = "", payload: dict | None = None) -> None:
        self._confirmations[self.function_call_id] = ToolConfirmation(hint=hint, payload=payload)


def _make_task(state: TaskState, text: str = "", hitl_data: list[dict] | None = None) -> Task:
    """Build a minimal Task with the given state and optional text/HITL data."""
    parts: list[A2APart] = []
    if hitl_data:
        for d in hitl_data:
            parts.append(
                A2APart(
                    root=DataPart(
                        data=d,
                        metadata={"adk_type": "function_call", "adk_is_long_running": True},
                    )
                )
            )
    elif text:
        parts.append(A2APart(root=TextPart(text=text)))

    status_message = A2AMessage(role=Role.agent, message_id="msg-1", parts=parts) if parts else None
    return Task(
        id="task-1",
        context_id="ctx-1",
        status=TaskStatus(state=state, message=status_message),
    )


def _make_hitl_task(tool_name: str = "delete_file", tool_call_id: str = "call_1") -> Task:
    """Build a task in input_required state with one HITL part."""
    hitl_data = [
        {
            "name": "adk_request_confirmation",
            "id": "conf_1",
            "args": {
                "originalFunctionCall": {
                    "name": tool_name,
                    "args": {"path": "/tmp/x"},
                    "id": tool_call_id,
                },
            },
        }
    ]
    return _make_task(TaskState.input_required, hitl_data=hitl_data)


async def _async_yield(*items) -> AsyncIterator:
    """Yield items from an async generator (simulates client.send_message)."""
    for item in items:
        yield item


def _make_tool(*, httpx_client: httpx.AsyncClient | None = None) -> KAgentRemoteA2ATool:
    return KAgentRemoteA2ATool(
        name="k8s_agent",
        description="K8s subagent",
        agent_card_url="http://k8s-agent/.well-known/agent.json",
        httpx_client=httpx_client,
    )


def _patch_client(tool: KAgentRemoteA2ATool, send_side_effect):
    """Patch _ensure_client on *tool* so send_message uses *send_side_effect*.

    *send_side_effect* is either a callable (async generator function)
    or an async-iterable return value.
    """
    p = patch.object(tool, "_ensure_client")
    mock_ensure = p.start()
    mock_client = MagicMock()
    if callable(send_side_effect) and not isinstance(send_side_effect, MagicMock):
        mock_client.send_message = send_side_effect
    else:
        mock_client.send_message = MagicMock(return_value=send_side_effect)
    mock_ensure.return_value = mock_client
    return p, mock_client


def _approval_ctx(confirmed: bool, payload: dict | None = None, **kwargs) -> MockToolContext:
    confirmation = ToolConfirmation(confirmed=confirmed, payload=payload or {})
    return MockToolContext(tool_confirmation=confirmation, **kwargs)


# ---------------------------------------------------------------------------
# _SubagentInterceptor header propagation tests
# ---------------------------------------------------------------------------


class TestSubagentInterceptorHeaderPropagation:
    """Tests for header propagation in _SubagentInterceptor via context state."""

    async def _call_intercept(self, interceptor, state: dict) -> dict:
        from a2a.client.middleware import ClientCallContext

        ctx = ClientCallContext(state=state)
        _, http_kwargs = await interceptor.intercept(
            method_name="message/send",
            request_payload={},
            http_kwargs={},
            agent_card=None,
            context=ctx,
        )
        return http_kwargs.get("headers", {})

    async def test_forwards_extra_headers_from_context_state(self):
        interceptor = _SubagentInterceptor()
        headers = await self._call_intercept(
            interceptor,
            state={"x-user-id": "user1", "_a2a_extra_headers": {"authorization": "Bearer test-jwt"}},
        )
        assert headers.get("authorization") == "Bearer test-jwt"

    async def test_no_extra_headers_without_state_key(self):
        interceptor = _SubagentInterceptor()
        headers = await self._call_intercept(
            interceptor,
            state={"x-user-id": "user1", "authorization": "Bearer test-jwt"},
        )
        assert "authorization" not in headers


# ---------------------------------------------------------------------------
# First-call tests
# ---------------------------------------------------------------------------


class TestFirstCall:
    """Tests for the initial tool invocation (Phase 1)."""

    async def test_completed_task_returns_result_with_session_id(self):
        """Completed task returns dict with result text and subagent_session_id."""
        tool = _make_tool()
        task = _make_task(TaskState.completed, text="all done")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            result = await tool.run_async(args={"request": "do something"}, tool_context=MockToolContext())
        finally:
            p.stop()

        assert isinstance(result, dict)
        assert result["result"] == "all done"
        assert result["subagent_session_id"] == tool._last_context_id

    async def test_direct_message_response_returns_text(self):
        """When remote agent returns an A2AMessage directly, result is plain text."""
        tool = _make_tool()
        msg = A2AMessage(
            role=Role.agent,
            message_id="m1",
            parts=[A2APart(root=TextPart(text="direct reply"))],
        )
        p, _ = _patch_client(tool, _async_yield(msg))
        try:
            result = await tool.run_async(args={"request": "hi"}, tool_context=MockToolContext())
        finally:
            p.stop()

        assert result == "direct reply"

    async def test_no_result_returns_fallback_string(self):
        """When remote agent yields nothing, a fallback error string is returned."""
        tool = _make_tool()
        p, _ = _patch_client(tool, _async_yield())
        try:
            result = await tool.run_async(args={"request": "hi"}, tool_context=MockToolContext())
        finally:
            p.stop()

        assert "no result" in result.lower()

    async def test_failed_task_returns_error_text(self):
        """Failed tasks return the error text from the task status message."""
        tool = _make_tool()
        task = _make_task(TaskState.failed, text="something broke")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            result = await tool.run_async(args={"request": "go"}, tool_context=MockToolContext())
        finally:
            p.stop()

        assert result == "something broke"

    async def test_context_id_sent_in_outgoing_message(self):
        """The tool's pre-generated context_id is sent on the outgoing A2A message."""
        tool = _make_tool()
        task = _make_task(TaskState.completed, text="ok")
        sent: list[A2AMessage] = []

        async def capture(*, request, **kw):
            sent.append(request)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            await tool.run_async(args={"request": "hello"}, tool_context=MockToolContext())
        finally:
            p.stop()

        assert sent[0].context_id == tool._last_context_id

    async def test_user_id_forwarded_in_call_context(self):
        """The parent session's user_id is forwarded via ClientCallContext."""
        tool = _make_tool()
        task = _make_task(TaskState.completed, text="ok")
        captured_contexts: list = []

        async def capture(*, request, context=None, **kw):
            captured_contexts.append(context)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            ctx = MockToolContext(user_id="alice@example.com")
            await tool.run_async(args={"request": "go"}, tool_context=ctx)
        finally:
            p.stop()

        assert captured_contexts[0].state["x-user-id"] == "alice@example.com"


# ---------------------------------------------------------------------------
# HITL input_required tests
# ---------------------------------------------------------------------------


class TestHITLInputRequired:
    """Tests for when the subagent returns input_required."""

    async def test_calls_request_confirmation(self):
        """request_confirmation is called with a hint naming the inner tool."""
        tool = _make_tool()
        task = _make_hitl_task(tool_name="delete_file")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            ctx = MockToolContext()
            await tool.run_async(args={"request": "delete it"}, tool_context=ctx)
        finally:
            p.stop()

        assert ctx.function_call_id in ctx._confirmations
        conf = ctx._confirmations[ctx.function_call_id]
        assert "delete_file" in conf.hint

    async def test_confirmation_payload(self):
        """Payload contains task_id, context_id, subagent_name, and hitl_parts."""
        tool = _make_tool()
        task = _make_hitl_task(tool_name="write_file", tool_call_id="c99")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            ctx = MockToolContext()
            await tool.run_async(args={"request": "go"}, tool_context=ctx)
        finally:
            p.stop()

        payload = ctx._confirmations[ctx.function_call_id].payload
        assert payload["task_id"] == "task-1"
        assert payload["context_id"] == "ctx-1"
        assert payload["subagent_name"] == "k8s_agent"
        # hitl_parts should contain the serialized HITL info
        hitl_parts = payload["hitl_parts"]
        assert len(hitl_parts) == 1
        assert hitl_parts[0]["originalFunctionCall"]["name"] == "write_file"
        assert hitl_parts[0]["originalFunctionCall"]["id"] == "c99"


# ---------------------------------------------------------------------------
# HITL resume tests (Phase 2)
# ---------------------------------------------------------------------------

_RESUME_PAYLOAD = {"task_id": "task-1", "context_id": "ctx-1", "subagent_name": "k8s_agent"}


class TestHITLResume:
    """Tests for resume after HITL confirmation (Phase 2)."""

    async def _resume(
        self,
        tool: KAgentRemoteA2ATool,
        confirmed: bool,
        payload: dict,
        response_task: Task | None = None,
    ) -> tuple[Any, list[A2AMessage]]:
        """Run a resume and return (result, sent_messages)."""
        if response_task is None:
            response_task = _make_task(TaskState.completed, text="ok")
        sent: list[A2AMessage] = []

        async def capture(*, request, **kw):
            sent.append(request)
            yield (response_task, None)

        p, _ = _patch_client(tool, capture)
        try:
            ctx = _approval_ctx(confirmed=confirmed, payload=payload)
            result = await tool.run_async(args={}, tool_context=ctx)
        finally:
            p.stop()
        return result, sent

    async def test_approve_sends_approve_decision(self):
        tool = _make_tool()
        result, sent = await self._resume(
            tool,
            confirmed=True,
            payload=_RESUME_PAYLOAD,
            response_task=_make_task(TaskState.completed, text="approved"),
        )
        assert result["result"] == "approved"
        data = sent[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_APPROVE
        # Verify task_id and context_id are routed correctly
        assert sent[0].task_id == "task-1"
        assert sent[0].context_id == "ctx-1"

    async def test_reject_sends_reject_decision(self):
        tool = _make_tool()
        _, sent = await self._resume(tool, confirmed=False, payload=_RESUME_PAYLOAD)
        data = sent[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_REJECT

    async def test_reject_with_reason(self):
        tool = _make_tool()
        payload = {**_RESUME_PAYLOAD, "rejection_reason": "Too risky"}
        _, sent = await self._resume(tool, confirmed=False, payload=payload)
        data = sent[0].parts[0].root.data
        assert data["rejection_reason"] == "Too risky"

    async def test_batch_decisions_forwarded(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "batch_decisions": {"call_1": "approve", "call_2": "reject"},
        }
        result, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = sent[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_BATCH
        assert data["decisions"] == {"call_1": "approve", "call_2": "reject"}

    async def test_batch_with_rejection_reasons(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "batch_decisions": {"call_1": "approve", "call_2": "reject"},
            "rejection_reasons": {"call_2": "Too dangerous"},
        }
        _, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = sent[0].parts[0].root.data
        assert data["rejection_reasons"] == {"call_2": "Too dangerous"}

    async def test_ask_user_answers_forwarded(self):
        """ask_user answers are forwarded as approve with ask_user_answers payload."""
        tool = _make_tool()
        payload = {**_RESUME_PAYLOAD, "answers": ["yes", "42"]}
        _, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = sent[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_APPROVE
        assert data["ask_user_answers"] == ["yes", "42"]

    async def test_missing_task_id_returns_error(self):
        """Resume without task_id in payload returns an error string."""
        tool = _make_tool()
        ctx = _approval_ctx(confirmed=True, payload={"context_id": "ctx-1"})
        result = await tool.run_async(args={}, tool_context=ctx)
        assert "missing task context" in result.lower()

    async def test_resume_returns_subagent_session_id(self):
        """Resume result includes the subagent_session_id from the confirmation payload."""
        tool = _make_tool()
        result, _ = await self._resume(tool, confirmed=True, payload=_RESUME_PAYLOAD)
        assert result["subagent_session_id"] == "ctx-1"

    async def test_resume_input_required_chains(self):
        """If the subagent returns input_required again after resume, it chains."""
        tool = _make_tool()
        chained_task = _make_hitl_task(tool_name="restart_pod")
        p, _ = _patch_client(tool, _async_yield((chained_task, None)))
        try:
            ctx = _approval_ctx(confirmed=True, payload=_RESUME_PAYLOAD)
            result = await tool.run_async(args={}, tool_context=ctx)
        finally:
            p.stop()

        assert result["waiting_for"] == "subagent_approval"
        assert ctx.function_call_id in ctx._confirmations
        assert "restart_pod" in ctx._confirmations[ctx.function_call_id].hint


# ---------------------------------------------------------------------------
# Toolset lifecycle tests
# ---------------------------------------------------------------------------


class TestToolsetLifecycle:
    async def test_close_closes_owned_client(self):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        toolset = KAgentRemoteA2AToolset(
            name="agent",
            description="desc",
            agent_card_url="http://agent/.well-known/agent.json",
            httpx_client=mock_client,
        )
        await toolset.close()
        mock_client.aclose.assert_awaited_once()
        assert toolset._httpx_client is None

    async def test_close_is_idempotent(self):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        toolset = KAgentRemoteA2AToolset(
            name="agent",
            description="desc",
            agent_card_url="http://agent/.well-known/agent.json",
            httpx_client=mock_client,
        )
        await toolset.close()
        await toolset.close()
        mock_client.aclose.assert_awaited_once()

    async def test_get_tools_returns_the_tool(self):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        toolset = KAgentRemoteA2AToolset(
            name="my_agent",
            description="desc",
            agent_card_url="http://agent/.well-known/agent.json",
            httpx_client=mock_client,
        )
        tools = await toolset.get_tools()
        assert len(tools) == 1
        assert isinstance(tools[0], KAgentRemoteA2ATool)
        assert tools[0].name == "my_agent"
        await mock_client.aclose()
