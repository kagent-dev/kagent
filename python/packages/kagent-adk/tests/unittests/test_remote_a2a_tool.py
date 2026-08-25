"""Tests for KAgentRemoteA2ATool."""

from typing import Any, AsyncIterator, Callable, cast
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
from a2a.types import Message as A2AMessage
from a2a.types import Part as A2APart
from a2a.types import (
    Role,
    SendMessageRequest,
    StreamResponse,
    Task,
    TaskState,
    TaskStatus,
)
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.adk.tools.tool_context import ToolContext
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    HITL_EXTENSION_HEADER,
    HitlTool,
    ToolApprovalRequest,
    attach_hitl_extension,
    get_hitl_payload,
)

from kagent.adk._remote_a2a_tool import (
    KAgentRemoteA2ATool,
    KAgentRemoteA2AToolset,
)

# ---------------------------------------------------------------------------
# Test helpers
# ---------------------------------------------------------------------------

_DEFAULT_USER_ID = "admin@kagent.dev"


class _MockSession:
    """Minimal session mock providing user_id, id, and state."""

    def __init__(
        self,
        user_id: str = _DEFAULT_USER_ID,
        session_id: str | None = None,
        state: dict[str, Any] | None = None,
    ):
        self.user_id = user_id
        self.id = session_id
        self.state = state if state is not None else {}


class MockToolContext:
    """Minimal ToolContext mock matching the interface used by KAgentRemoteA2ATool."""

    def __init__(
        self,
        tool_confirmation: ToolConfirmation | None = None,
        user_id: str = _DEFAULT_USER_ID,
        session_id: str | None = None,
        session_state: dict[str, Any] | None = None,
    ):
        self.state: dict[str, Any] = {}
        self.function_call_id = "outer_fc_1"
        self.tool_confirmation = tool_confirmation
        self.session = _MockSession(user_id, session_id=session_id, state=session_state)
        self._confirmations: dict[str, ToolConfirmation] = {}

    def request_confirmation(self, *, hint: str = "", payload: dict | None = None) -> None:
        self._confirmations[self.function_call_id] = ToolConfirmation(hint=hint, payload=payload)

    def as_tool_context(self) -> ToolContext:
        return cast(ToolContext, self)


def _make_task(state: TaskState, text: str = "", hitl_data: list[dict] | None = None) -> Task:
    """Build a minimal Task with the given state and optional text/HITL data."""
    parts: list[A2APart] = []
    if text:
        parts.append(A2APart(text=text))

    status_message = A2AMessage(role=Role.ROLE_AGENT, message_id="msg-1", parts=parts) if parts else None
    if hitl_data:
        tools = []
        for data in hitl_data:
            original = data["args"]["originalFunctionCall"]
            tools.append(
                HitlTool(
                    id=data.get("id", ""),
                    call_id=original["id"],
                    name=original["name"],
                    args=original.get("args", {}),
                )
            )
        status_message = attach_hitl_extension(
            A2AMessage(role=Role.ROLE_AGENT, message_id="msg-1"),
            ToolApprovalRequest(tools=tools),
        )
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
    return _make_task(TaskState.TASK_STATE_INPUT_REQUIRED, hitl_data=hitl_data)


async def _async_yield(*items) -> AsyncIterator:
    """Yield items from an async generator (simulates client.send_message)."""
    for item in items:
        if isinstance(item, tuple):
            task, _ = item
            yield StreamResponse(task=task)
        elif isinstance(item, A2AMessage):
            yield StreamResponse(message=item)
        elif isinstance(item, Task):
            yield StreamResponse(task=item)
        else:
            yield item


def _make_tool(
    *,
    httpx_client: httpx.AsyncClient | None = None,
    header_provider: Callable[[Any], dict[str, str]] | None = None,
    isolate_sessions: bool = False,
) -> KAgentRemoteA2ATool:
    return KAgentRemoteA2ATool(
        name="k8s_agent",
        description="K8s subagent",
        agent_card_url="http://k8s-agent/.well-known/agent.json",
        httpx_client=httpx_client,
        header_provider=header_provider,
        isolate_sessions=isolate_sessions,
    )


def _patch_client(tool: KAgentRemoteA2ATool, send_side_effect):
    """Patch _ensure_client on *tool* so send_message uses *send_side_effect*.

    *send_side_effect* is either a callable (async generator function)
    or an async-iterable return value.
    """
    p = patch.object(tool, "_ensure_client")
    mock_ensure = p.start()
    mock_client = MagicMock()

    async def _wrap_stream(iterable):
        async for item in iterable:
            if isinstance(item, tuple):
                task, _ = item
                yield StreamResponse(task=task)
            elif isinstance(item, A2AMessage):
                yield StreamResponse(message=item)
            elif isinstance(item, Task):
                yield StreamResponse(task=item)
            else:
                yield item

    if callable(send_side_effect) and not isinstance(send_side_effect, MagicMock):

        def _invoke(*args, **kwargs):
            return _wrap_stream(send_side_effect(*args, **kwargs))

        mock_client.send_message = _invoke
    else:
        mock_client.send_message = MagicMock(return_value=_wrap_stream(send_side_effect))
    mock_ensure.return_value = mock_client
    return p, mock_client


def _approval_ctx(confirmed: bool, payload: dict | None = None, **kwargs) -> MockToolContext:
    confirmation = ToolConfirmation(confirmed=confirmed, payload=payload or {})
    return MockToolContext(tool_confirmation=confirmation, **kwargs)


# ---------------------------------------------------------------------------
# Call context header propagation tests
# ---------------------------------------------------------------------------


class TestCallContextHeaderPropagation:
    """Tests for header propagation via ClientCallContext.service_parameters."""

    async def test_forwards_extra_headers_from_header_provider(self):
        tool = KAgentRemoteA2ATool(
            name="k8s_agent",
            description="K8s subagent",
            agent_card_url="http://k8s-agent/.well-known/agent.json",
            header_provider=lambda _: {"authorization": "Bearer test-jwt"},
        )
        ctx = MockToolContext(user_id="user1")
        headers = tool._build_call_context(ctx.as_tool_context()).service_parameters or {}
        assert headers.get("authorization") == "Bearer test-jwt"
        assert headers.get("x-user-id") == "user1"
        assert headers.get(HITL_EXTENSION_HEADER) == "https://kagent.dev/extensions/hitl/v1"

    async def test_no_extra_headers_without_header_provider(self):
        tool = _make_tool()
        ctx = MockToolContext(user_id="user1")
        headers = tool._build_call_context(ctx.as_tool_context()).service_parameters or {}
        assert "authorization" not in headers


# ---------------------------------------------------------------------------
# First-call tests
# ---------------------------------------------------------------------------


class TestFirstCall:
    """Tests for the initial tool invocation (Phase 1)."""

    async def test_completed_task_returns_result_with_session_id(self):
        """Completed task returns dict with result text and subagent_session_id."""
        tool = _make_tool()
        task = _make_task(TaskState.TASK_STATE_COMPLETED, text="all done")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            result = await tool.run_async(
                args={"request": "do something"}, tool_context=MockToolContext().as_tool_context()
            )
        finally:
            p.stop()

        assert isinstance(result, dict)
        assert result["result"] == "all done"
        assert result["subagent_session_id"] == tool._last_context_id

    async def test_direct_message_response_returns_text(self):
        """When remote agent returns an A2AMessage directly, result is plain text."""
        tool = _make_tool()
        msg = A2AMessage(
            role=Role.ROLE_AGENT,
            message_id="m1",
            parts=[A2APart(text="direct reply")],
        )
        p, _ = _patch_client(tool, _async_yield(msg))
        try:
            result = await tool.run_async(args={"request": "hi"}, tool_context=MockToolContext().as_tool_context())
        finally:
            p.stop()

        assert result == "direct reply"

    async def test_no_result_returns_fallback_string(self):
        """When remote agent yields nothing, a fallback error string is returned."""
        tool = _make_tool()
        p, _ = _patch_client(tool, _async_yield())
        try:
            result = await tool.run_async(args={"request": "hi"}, tool_context=MockToolContext().as_tool_context())
        finally:
            p.stop()

        assert "no result" in result.lower()

    async def test_failed_task_returns_error_text(self):
        """Failed tasks return the error text from the task status message."""
        tool = _make_tool()
        task = _make_task(TaskState.TASK_STATE_FAILED, text="something broke")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            result = await tool.run_async(args={"request": "go"}, tool_context=MockToolContext().as_tool_context())
        finally:
            p.stop()

        assert result == "something broke"

    async def test_context_id_sent_in_outgoing_message(self):
        """The tool's pre-generated context_id is sent on the outgoing A2A message."""
        tool = _make_tool()
        task = _make_task(TaskState.TASK_STATE_COMPLETED, text="ok")
        sent: list[SendMessageRequest] = []

        async def capture(*, request: SendMessageRequest, **kw):
            sent.append(request)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            await tool.run_async(args={"request": "hello"}, tool_context=MockToolContext().as_tool_context())
        finally:
            p.stop()

        assert sent[0].message.context_id == tool._last_context_id

    async def test_shared_session_reuses_context_id_across_calls(self):
        """The default mode keeps stateful sub-agent calls in one session."""
        tool = _make_tool()
        task = _make_task(TaskState.TASK_STATE_COMPLETED, text="ok")
        sent: list[SendMessageRequest] = []

        async def capture(*, request: SendMessageRequest, **kw):
            sent.append(request)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            context = MockToolContext().as_tool_context()
            await tool.run_async(args={"request": "first"}, tool_context=context)
            await tool.run_async(args={"request": "second"}, tool_context=context)
        finally:
            p.stop()

        assert len(sent) == 2
        assert sent[0].message.context_id == sent[1].message.context_id

    async def test_isolated_session_mints_context_id_per_call(self):
        """Isolation gives each sub-agent invocation a distinct session."""
        tool = _make_tool(isolate_sessions=True)
        task = _make_task(TaskState.TASK_STATE_COMPLETED, text="ok")
        sent: list[SendMessageRequest] = []

        async def capture(*, request: SendMessageRequest, **kw):
            sent.append(request)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            context = MockToolContext().as_tool_context()
            first = await tool.run_async(args={"request": "first"}, tool_context=context)
            second = await tool.run_async(args={"request": "second"}, tool_context=context)
        finally:
            p.stop()

        assert len(sent) == 2
        assert sent[0].message.context_id != sent[1].message.context_id
        assert first["subagent_session_id"] == sent[0].message.context_id
        assert second["subagent_session_id"] == sent[1].message.context_id

    async def test_user_id_forwarded_in_call_context(self):
        """The parent session's user_id is forwarded via ClientCallContext."""
        tool = _make_tool()
        task = _make_task(TaskState.TASK_STATE_COMPLETED, text="ok")
        captured_contexts: list = []

        async def capture(*, request, context=None, **kw):
            captured_contexts.append(context)
            yield (task, None)

        p, _ = _patch_client(tool, capture)
        try:
            ctx = MockToolContext(user_id="alice@example.com")
            await tool.run_async(args={"request": "go"}, tool_context=ctx.as_tool_context())
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
            await tool.run_async(args={"request": "delete it"}, tool_context=ctx.as_tool_context())
        finally:
            p.stop()

        assert ctx.function_call_id in ctx._confirmations
        conf = ctx._confirmations[ctx.function_call_id]
        assert "delete_file" in conf.hint

    async def test_confirmation_payload(self):
        """Payload retains the child task routing and validated public request."""
        tool = _make_tool()
        task = _make_hitl_task(tool_name="write_file", tool_call_id="c99")
        p, _ = _patch_client(tool, _async_yield((task, None)))
        try:
            ctx = MockToolContext()
            await tool.run_async(args={"request": "go"}, tool_context=ctx.as_tool_context())
        finally:
            p.stop()

        payload = ctx._confirmations[ctx.function_call_id].payload
        assert payload is not None
        assert payload["task_id"] == "task-1"
        assert payload["context_id"] == "ctx-1"
        assert payload["subagent_name"] == "k8s_agent"
        request = payload["hitl_request"]
        assert request["type"] == "tool_approval_request"
        assert request["tools"][0]["name"] == "write_file"
        assert request["tools"][0]["call_id"] == "c99"


# ---------------------------------------------------------------------------
# HITL resume tests (Phase 2)
# ---------------------------------------------------------------------------

_RESUME_PAYLOAD = {
    "task_id": "task-1",
    "context_id": "ctx-1",
    "subagent_name": "k8s_agent",
    "hitl_request": {
        "type": "tool_approval_request",
        "tools": [{"id": "conf_1", "call_id": "call_1", "name": "delete_file", "args": {}}],
    },
    "hitl_response": {
        "type": "tool_approval_response",
        "approvals": [{"id": "conf_1", "approved": True}],
    },
}


class TestHITLResume:
    """Tests for resume after HITL confirmation (Phase 2)."""

    async def _resume(
        self,
        tool: KAgentRemoteA2ATool,
        confirmed: bool,
        payload: dict,
        response_task: Task | None = None,
    ) -> tuple[Any, list[SendMessageRequest]]:
        """Run a resume and return (result, sent_messages)."""
        if response_task is None:
            response_task = _make_task(TaskState.TASK_STATE_COMPLETED, text="ok")
        sent: list[SendMessageRequest] = []

        async def capture(*, request: SendMessageRequest, **kw):
            sent.append(request)
            yield (response_task, None)

        p, _ = _patch_client(tool, capture)
        try:
            ctx = _approval_ctx(confirmed=confirmed, payload=payload)
            result = await tool.run_async(args={}, tool_context=ctx.as_tool_context())
        finally:
            p.stop()
        return result, sent

    async def test_approve_sends_approve_decision(self):
        tool = _make_tool()
        result, sent = await self._resume(
            tool,
            confirmed=True,
            payload=_RESUME_PAYLOAD,
            response_task=_make_task(TaskState.TASK_STATE_COMPLETED, text="approved"),
        )
        assert result["result"] == "approved"
        data = get_hitl_payload(sent[0].message)
        assert data["approvals"] == [{"id": "conf_1", "approved": True}]
        # Verify task_id and context_id are routed correctly
        assert sent[0].message.task_id == "task-1"
        assert sent[0].message.context_id == "ctx-1"

    async def test_reject_sends_reject_decision(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "hitl_response": {
                "type": "tool_approval_response",
                "approvals": [{"id": "conf_1", "approved": False}],
            },
        }
        _, sent = await self._resume(tool, confirmed=False, payload=payload)
        data = get_hitl_payload(sent[0].message)
        assert data["approvals"] == [{"id": "conf_1", "approved": False}]

    async def test_reject_with_reason(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "hitl_response": {
                "type": "tool_approval_response",
                "approvals": [{"id": "conf_1", "approved": False, "rejection_reason": "Too risky"}],
            },
        }
        _, sent = await self._resume(tool, confirmed=False, payload=payload)
        data = get_hitl_payload(sent[0].message)
        assert data["approvals"][0]["rejection_reason"] == "Too risky"

    async def test_multiple_approvals_forwarded(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "hitl_request": {
                "type": "tool_approval_request",
                "tools": [
                    *_RESUME_PAYLOAD["hitl_request"]["tools"],
                    {"id": "conf_2", "call_id": "call_2", "name": "restart_pod", "args": {}},
                ],
            },
            "hitl_response": {
                "type": "tool_approval_response",
                "approvals": [
                    {"id": "conf_1", "approved": True},
                    {"id": "conf_2", "approved": False},
                ],
            },
        }
        result, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = get_hitl_payload(sent[0].message)
        assert data["approvals"] == [
            {"id": "conf_1", "approved": True},
            {"id": "conf_2", "approved": False},
        ]

    async def test_multiple_approvals_with_rejection_reason(self):
        tool = _make_tool()
        payload = {
            **_RESUME_PAYLOAD,
            "hitl_request": {
                "type": "tool_approval_request",
                "tools": [
                    *_RESUME_PAYLOAD["hitl_request"]["tools"],
                    {"id": "conf_2", "call_id": "call_2", "name": "restart_pod", "args": {}},
                ],
            },
            "hitl_response": {
                "type": "tool_approval_response",
                "approvals": [
                    {"id": "conf_1", "approved": True},
                    {"id": "conf_2", "approved": False, "rejection_reason": "Too dangerous"},
                ],
            },
        }
        _, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = get_hitl_payload(sent[0].message)
        assert data["approvals"][1] == {"id": "conf_2", "approved": False, "rejection_reason": "Too dangerous"}

    async def test_ask_user_answers_forwarded(self):
        """ask_user answers are forwarded through the A2A extension."""
        tool = _make_tool()
        answers = [{"answer": ["yes"]}, {"answer": ["42"]}]
        payload = {
            **_RESUME_PAYLOAD,
            "hitl_request": {
                "type": "ask_user_request",
                "id": "conf_1",
                "questions": [{"question": "Continue?"}, {"question": "Value?"}],
            },
            "hitl_response": {
                "type": "ask_user_response",
                "id": "conf_1",
                "answers": answers,
            },
        }
        _, sent = await self._resume(tool, confirmed=True, payload=payload)
        data = get_hitl_payload(sent[0].message)
        assert data["type"] == "ask_user_response"
        assert data["id"] == "conf_1"
        assert data["answers"] == answers

    async def test_missing_task_id_returns_error(self):
        """Resume without task_id in payload returns an error string."""
        tool = _make_tool()
        ctx = _approval_ctx(confirmed=True, payload={"context_id": "ctx-1"})
        result = await tool.run_async(args={}, tool_context=ctx.as_tool_context())
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
            result = await tool.run_async(args={}, tool_context=ctx.as_tool_context())
        finally:
            p.stop()

        assert result["waiting_for"] == "subagent_approval"
        assert ctx.function_call_id in ctx._confirmations
        assert "restart_pod" in ctx._confirmations[ctx.function_call_id].hint


# ---------------------------------------------------------------------------
# Toolset lifecycle tests
# ---------------------------------------------------------------------------


class TestToolsetLifecycle:
    async def test_isolate_sessions_is_retained_by_toolset(self):
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        toolset = KAgentRemoteA2AToolset(
            name="agent",
            description="desc",
            agent_card_url="http://agent/.well-known/agent.json",
            httpx_client=mock_client,
            isolate_sessions=True,
        )
        assert toolset._tool._isolate_sessions is True
        await toolset.close()

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


# ---------------------------------------------------------------------------
# Conversation lineage header tests
# ---------------------------------------------------------------------------


class TestLineageHeaderPropagation:
    """Tests for the parent/root context_id headers built by
    ``KAgentRemoteA2ATool._build_call_context``.

    The lineage headers let a remote A2A peer correlate this turn with the
    originating chat conversation. ``x-kagent-parent-context-id`` is always
    the immediate caller's session id; ``x-kagent-root-context-id`` is
    forwarded unchanged from the upstream caller when present, or stamped
    with the immediate caller's own id when this agent is the root of the
    chain.
    """

    def _build_headers(self, tool: KAgentRemoteA2ATool, ctx: MockToolContext) -> dict[str, str]:
        return tool._build_call_context(ctx.as_tool_context()).service_parameters or {}

    def test_root_agent_stamps_own_id_as_root_and_parent(self):
        """An agent at the top of the chain (no inbound lineage headers) sets
        both parent and root to its own session id."""
        tool = _make_tool()
        ctx = MockToolContext(session_id="chat-1", session_state={"headers": {}})

        headers = self._build_headers(tool, ctx)

        assert headers.get("x-kagent-parent-context-id") == "chat-1"
        assert headers.get("x-kagent-root-context-id") == "chat-1"

    def test_mid_chain_forwards_root_and_overrides_parent(self):
        """An agent in the middle of an A2A chain forwards the root header
        unchanged from the inbound request and replaces parent with its own
        session id."""
        tool = _make_tool()
        ctx = MockToolContext(
            session_id="router-2",
            session_state={
                "headers": {
                    "x-kagent-parent-context-id": "chat-1",
                    "x-kagent-root-context-id": "chat-1",
                }
            },
        )

        headers = self._build_headers(tool, ctx)

        assert headers.get("x-kagent-parent-context-id") == "router-2"
        assert headers.get("x-kagent-root-context-id") == "chat-1"

    def test_inbound_parent_only_does_not_seed_root(self):
        """An inbound parent header alone is not used to derive root: both
        lineage headers are introduced together, so a request carrying only a
        parent header is not a real upstream root. Root falls back to our own
        session id instead."""
        tool = _make_tool()
        ctx = MockToolContext(
            session_id="router-2",
            session_state={"headers": {"x-kagent-parent-context-id": "ignored-1"}},
        )

        headers = self._build_headers(tool, ctx)

        assert headers.get("x-kagent-parent-context-id") == "router-2"
        assert headers.get("x-kagent-root-context-id") == "router-2"

    def test_no_session_id_emits_no_lineage_headers(self):
        """When the caller cannot resolve a session id (e.g. a stub
        ToolContext), the outbound request gets no lineage headers — matches
        pre-feature behavior so this change is non-breaking for callers that
        don't yet plumb session ids."""
        tool = _make_tool()
        ctx = MockToolContext(session_id=None, session_state={"headers": {}})

        headers = self._build_headers(tool, ctx)

        assert "x-kagent-parent-context-id" not in headers and "x-kagent-root-context-id" not in headers

    def test_header_provider_overrides_lineage(self):
        """A constructor-supplied header_provider can override lineage
        headers — escape hatch for custom propagation logic."""
        tool = _make_tool(header_provider=lambda _ctx: {"x-kagent-root-context-id": "forced"})
        ctx = MockToolContext(session_id="router-2", session_state={"headers": {}})

        headers = self._build_headers(tool, ctx)

        assert headers.get("x-kagent-parent-context-id") == "router-2"
        assert headers.get("x-kagent-root-context-id") == "forced"
