"""Tests for the public A2A HITL Message extension helpers."""

from a2a.types import Message, Role

from kagent.core.a2a import (
    HITL_EXTENSION_URI,
    AskUserRequest,
    HitlTool,
    ToolApproval,
    ToolApprovalRequest,
    ToolApprovalResponse,
    attach_hitl_extension,
    get_ask_user_request,
    get_hitl_payload,
    get_tool_approval_request,
    get_tool_approval_response,
    hitl_activated,
)


def test_attach_and_parse_decision() -> None:
    message = Message(role=Role.ROLE_USER, message_id="m", task_id="t", context_id="c")
    attach_hitl_extension(message, ToolApprovalResponse(approvals=[ToolApproval(id="approval-1", approved=True)]))

    assert message.extensions == [HITL_EXTENSION_URI]
    assert get_hitl_payload(message) == {
        "type": "tool_approval_response",
        "approvals": [{"id": "approval-1", "approved": True}],
    }
    assert get_tool_approval_response(message).approvals[0].approved


def test_parser_requires_declared_extension() -> None:
    message = Message(role=Role.ROLE_USER, message_id="m")
    assert get_hitl_payload(message) is None
    assert get_tool_approval_response(message) is None


def test_activation_requires_exact_uri() -> None:
    assert hitl_activated({"A2A-Extensions": HITL_EXTENSION_URI})
    assert hitl_activated({"a2a-extensions": f"other, {HITL_EXTENSION_URI}"})
    assert not hitl_activated({"A2A-Extensions": "https://kagent.dev/extensions/hitl/v2"})


def test_tool_approval_request_has_per_tool_correlation() -> None:
    request = ToolApprovalRequest(
        tools=[HitlTool(id="confirmation-1", call_id="call-1", name="delete_file")],
    )

    assert request.model_dump(exclude_none=True)["tools"] == [
        {"id": "confirmation-1", "call_id": "call-1", "name": "delete_file", "args": {}}
    ]


def test_parse_tool_approval_request() -> None:
    message = Message(role=Role.ROLE_AGENT, message_id="m", task_id="t", context_id="c")
    attach_hitl_extension(
        message,
        ToolApprovalRequest(
            tools=[HitlTool(id="confirmation-1", call_id="call-1", name="delete_file")],
        ),
    )

    request = get_tool_approval_request(message)

    assert request is not None
    assert request.tools[0].id == "confirmation-1"


def test_parse_ask_user_request() -> None:
    message = Message(role=Role.ROLE_AGENT, message_id="m", task_id="t", context_id="c")
    attach_hitl_extension(
        message,
        AskUserRequest(id="question-1", questions=[{"question": "Which namespace?"}]),
    )

    request = get_ask_user_request(message)

    assert request is not None
    assert request.id == "question-1"
