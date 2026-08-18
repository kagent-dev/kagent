"""Tests for the LangGraph executor's interrupt handling."""

from unittest.mock import AsyncMock, MagicMock

from kagent.core.a2a import get_tool_approval_request
from kagent.langgraph._executor import LangGraphAgentExecutor


def _executor() -> LangGraphAgentExecutor:
    return LangGraphAgentExecutor(graph=MagicMock(), app_name="test__NS__agent")


def _interrupt(*names: str) -> list[dict]:
    return [{"action_requests": [{"id": f"call-{name}", "name": name, "args": {"path": f"/{name}"}} for name in names]}]


async def _handle(hitl_enabled: bool, *names: str):
    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()
    await _executor()._handle_interrupt(
        interrupt_data=_interrupt(*names),
        task_id="task-1",
        context_id="context-1",
        event_queue=event_queue,
        hitl_enabled=hitl_enabled,
    )
    event_queue.enqueue_event.assert_awaited_once()
    return event_queue.enqueue_event.await_args.args[0].status.message


def _text(message) -> str:
    return "".join(part.text for part in message.parts if part.HasField("text"))


async def test_interrupt_without_activation_names_the_tools():
    message = await _handle(False, "delete_file", "restart_pod")

    assert get_tool_approval_request(message) is None
    assert _text(message) == "Approval is required for tool(s): delete_file, restart_pod"


async def test_interrupt_with_activation_keeps_the_same_text():
    message = await _handle(True, "delete_file")

    request = get_tool_approval_request(message)

    assert request is not None
    assert request.tools[0].name == "delete_file"
    assert request.hint == "Approval is required for tool(s): delete_file"
    assert _text(message) == "Approval is required for tool(s): delete_file"
