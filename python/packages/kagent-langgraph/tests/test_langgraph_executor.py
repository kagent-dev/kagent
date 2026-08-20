"""Tests for the LangGraph executor's interrupt handling."""

from unittest.mock import AsyncMock, MagicMock

from kagent.core.a2a import get_ask_user_request, get_tool_approval_request

from kagent.langgraph._executor import LangGraphAgentExecutor


def _executor() -> LangGraphAgentExecutor:
    return LangGraphAgentExecutor(graph=MagicMock(), app_name="test__NS__agent")


def _interrupt(*names: str) -> list[dict]:
    return [{"action_requests": [{"id": f"call-{name}", "name": name, "args": {"path": f"/{name}"}} for name in names]}]


def _ask_user_interrupt(*questions: str) -> list[dict]:
    return [
        {
            "action_requests": [
                {
                    "id": "call-ask",
                    "name": "ask_user",
                    "args": {"questions": [{"question": question} for question in questions]},
                }
            ]
        }
    ]


async def _handle(hitl_enabled: bool, *names: str, interrupt_data: list[dict] | None = None):
    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()
    await _executor()._handle_interrupt(
        interrupt_data=interrupt_data or _interrupt(*names),
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


async def test_interrupt_ask_user_carries_the_questions():
    message = await _handle(True, interrupt_data=_ask_user_interrupt("Which namespace?", "Which cluster?"))

    request = get_ask_user_request(message)

    assert request is not None
    assert request.questions == [{"question": "Which namespace?"}, {"question": "Which cluster?"}]
    assert _text(message) == "Which namespace?; Which cluster?"


async def test_interrupt_ask_user_without_activation_still_asks():
    message = await _handle(False, interrupt_data=_ask_user_interrupt("Which namespace?"))

    assert get_ask_user_request(message) is None
    assert _text(message) == "Which namespace?"


async def test_interrupt_skips_action_requests_without_a_name_or_id():
    interrupt_data = [{"action_requests": [{"args": {"path": "/tmp"}}, {"id": "call-1"}, "not-a-mapping"]}]

    message = await _handle(True, interrupt_data=interrupt_data)

    assert get_tool_approval_request(message) is None
    assert _text(message) == "Human input is required before the agent can continue."


async def test_interrupt_normalizes_non_dict_args():
    interrupt_data = [{"action_requests": [{"id": "call-1", "name": "delete_file", "args": "/tmp/x"}]}]

    message = await _handle(True, interrupt_data=interrupt_data)

    request = get_tool_approval_request(message)

    assert request is not None
    assert request.tools[0].args == {}
    assert _text(message) == "Approval is required for tool(s): delete_file"
