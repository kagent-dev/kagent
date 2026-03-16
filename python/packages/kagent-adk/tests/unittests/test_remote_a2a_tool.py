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

from kagent.adk._remote_a2a_tool import KAgentRemoteA2ATool, KAgentRemoteA2AToolset
from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
)


def _make_task(state: TaskState, text: str = "", hitl_data: list[dict] | None = None) -> Task:
    """Build a minimal Task with the given state and optional text result.

    Text is placed in the status message (the fallback path in _extract_text_from_task)
    so tests don't need to construct Artifact objects with their required artifactId field.
    """
    parts: list[A2APart] = []

    if hitl_data:
        for d in hitl_data:
            parts.append(
                A2APart(
                    root=DataPart(
                        data=d,
                        metadata={
                            "adk_type": "function_call",
                            "adk_is_long_running": True,
                        },
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


class MockToolContext:
    """Minimal ToolContext mock matching the interface used by KAgentRemoteA2ATool."""

    def __init__(self, tool_confirmation: ToolConfirmation | None = None):
        self.state: dict[str, Any] = {}
        self.function_call_id = "outer_fc_1"
        self.tool_confirmation = tool_confirmation
        self._confirmations: dict[str, ToolConfirmation] = {}

    def request_confirmation(self, *, hint: str = "", payload: dict | None = None) -> None:
        self._confirmations[self.function_call_id] = ToolConfirmation(hint=hint, payload=payload)


def _make_tool(*, httpx_client: httpx.AsyncClient | None = None) -> KAgentRemoteA2ATool:
    return KAgentRemoteA2ATool(
        name="k8s_agent",
        description="K8s subagent",
        agent_card_url="http://k8s-agent/.well-known/agent.json",
        httpx_client=httpx_client,
    )


class TestDirectAgentCall:
    """
    Tests for direct agent call.
    This was the original behaviour of the AgentTool(RemoteA2aAgent(...)) pairing.
    """

    async def test_returns_artifact_text_on_completion(self):
        tool = _make_tool()
        task = _make_task(TaskState.completed, text="all done")

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield((task, None)))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            result = await tool.run_async(args={"request": "do something"}, tool_context=ctx)

        assert result == "all done"

    async def test_stores_context_id_after_completion(self):
        tool = _make_tool()
        task = Task(
            id="task-1",
            context_id="ctx-abc",
            status=TaskStatus(
                state=TaskState.completed,
                message=A2AMessage(
                    role=Role.agent,
                    message_id="msg-1",
                    parts=[A2APart(root=TextPart(text="ok"))],
                ),
            ),
        )

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield((task, None)))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            await tool.run_async(args={"request": "go"}, tool_context=ctx)

        assert tool._last_context_id == "ctx-abc"

    async def test_passes_stored_context_id_on_subsequent_call(self):
        tool = _make_tool()
        tool._last_context_id = "prev-ctx"
        task = _make_task(TaskState.completed, text="ok")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            await tool.run_async(args={"request": "hello"}, tool_context=ctx)

        assert sent_messages[0].context_id == "prev-ctx"

    async def test_direct_message_response_returns_text(self):
        tool = _make_tool()
        msg = A2AMessage(
            role=Role.agent,
            message_id="m1",
            context_id="ctx-direct",
            parts=[A2APart(root=TextPart(text="direct reply"))],
        )

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield(msg))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            result = await tool.run_async(args={"request": "hi"}, tool_context=ctx)

        assert result == "direct reply"
        assert tool._last_context_id == "ctx-direct"

    async def test_no_result_returns_fallback_string(self):
        tool = _make_tool()

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield())
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            result = await tool.run_async(args={"request": "hi"}, tool_context=ctx)

        assert "no result" in result.lower()


class TestHITLInputRequired:
    async def test_calls_request_confirmation_on_input_required(self):
        tool = _make_tool()
        task = _make_hitl_task(tool_name="delete_file", tool_call_id="call_1")

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield((task, None)))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            result = await tool.run_async(args={"request": "delete it"}, tool_context=ctx)

        # request_confirmation() should have been invoked
        assert ctx.function_call_id in ctx._confirmations
        conf = ctx._confirmations[ctx.function_call_id]
        assert "delete_file" in conf.hint

    async def test_confirmation_payload_contains_task_and_context_id(self):
        tool = _make_tool()
        task = _make_hitl_task()

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield((task, None)))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            await tool.run_async(args={"request": "go"}, tool_context=ctx)

        payload = ctx._confirmations[ctx.function_call_id].payload
        assert payload["task_id"] == "task-1"
        assert payload["context_id"] == "ctx-1"
        assert payload["subagent_name"] == "k8s_agent"

    async def test_confirmation_payload_contains_hitl_parts(self):
        tool = _make_tool()
        task = _make_hitl_task(tool_name="write_file", tool_call_id="c99")

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = MagicMock(return_value=_async_yield((task, None)))
            mock_ensure.return_value = mock_client

            ctx = MockToolContext()
            await tool.run_async(args={"request": "go"}, tool_context=ctx)

        payload = ctx._confirmations[ctx.function_call_id].payload
        hitl_parts = payload["hitl_parts"]
        assert hitl_parts is not None
        assert len(hitl_parts) == 1
        assert hitl_parts[0]["originalFunctionCall"]["name"] == "write_file"
        assert hitl_parts[0]["originalFunctionCall"]["id"] == "c99"


def _approval_ctx(confirmed: bool, payload: dict | None = None) -> MockToolContext:
    confirmation = ToolConfirmation(confirmed=confirmed, payload=payload or {})
    return MockToolContext(tool_confirmation=confirmation)


class TestHITLUniformDecisions:
    async def test_approve_sends_approve_decision(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="done after approve")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {"task_id": "task-1", "context_id": "ctx-1", "subagent_name": "k8s_agent"}
            ctx = _approval_ctx(confirmed=True, payload=payload)
            result = await tool.run_async(args={}, tool_context=ctx)

        assert result == "done after approve"
        assert len(sent_messages) == 1
        msg = sent_messages[0]
        data = msg.parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_APPROVE

    async def test_reject_sends_reject_decision(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="done after reject")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {"task_id": "task-1", "context_id": "ctx-1", "subagent_name": "k8s_agent"}
            ctx = _approval_ctx(confirmed=False, payload=payload)
            result = await tool.run_async(args={}, tool_context=ctx)

        assert result == "done after reject"
        data = sent_messages[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_REJECT

    async def test_reject_with_reason_forwards_reason(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="ok")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {
                "task_id": "task-1",
                "context_id": "ctx-1",
                "subagent_name": "k8s_agent",
                "rejection_reason": "Too risky",
            }
            ctx = _approval_ctx(confirmed=False, payload=payload)
            await tool.run_async(args={}, tool_context=ctx)

        data = sent_messages[0].parts[0].root.data
        assert data.get("rejection_reason") == "Too risky"

    async def test_resume_routes_to_correct_task_and_context(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="ok")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {"task_id": "task-99", "context_id": "ctx-99", "subagent_name": "k8s_agent"}
            ctx = _approval_ctx(confirmed=True, payload=payload)
            await tool.run_async(args={}, tool_context=ctx)

        msg = sent_messages[0]
        assert msg.task_id == "task-99"
        assert msg.context_id == "ctx-99"


class TestHITLBatchDecisions:
    async def test_batch_decisions_forwarded(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="batch done")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {
                "task_id": "task-1",
                "context_id": "ctx-1",
                "subagent_name": "k8s_agent",
                "batch_decisions": {"call_1": "approve", "call_2": "reject"},
            }
            ctx = _approval_ctx(confirmed=True, payload=payload)
            result = await tool.run_async(args={}, tool_context=ctx)

        assert result == "batch done"
        data = sent_messages[0].parts[0].root.data
        assert data[KAGENT_HITL_DECISION_TYPE_KEY] == KAGENT_HITL_DECISION_TYPE_BATCH
        assert data["decisions"] == {"call_1": "approve", "call_2": "reject"}

    async def test_batch_with_rejection_reasons_forwarded(self):
        tool = _make_tool()
        final_task = _make_task(TaskState.completed, text="ok")

        sent_messages: list[A2AMessage] = []

        async def capturing_send(*, request: A2AMessage):
            sent_messages.append(request)
            yield (final_task, None)

        with patch.object(tool, "_ensure_client") as mock_ensure:
            mock_client = MagicMock()
            mock_client.send_message = capturing_send
            mock_ensure.return_value = mock_client

            payload = {
                "task_id": "task-1",
                "context_id": "ctx-1",
                "subagent_name": "k8s_agent",
                "batch_decisions": {"call_1": "approve", "call_2": "reject"},
                "rejection_reasons": {"call_2": "Too dangerous"},
            }
            ctx = _approval_ctx(confirmed=True, payload=payload)
            await tool.run_async(args={}, tool_context=ctx)

        data = sent_messages[0].parts[0].root.data
        assert data.get("rejection_reasons") == {"call_2": "Too dangerous"}


class TestToolsetCloseLifecycle:
    """Tests for the toolset close lifecycle."""

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
        await toolset.close()  # second call must not raise

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
