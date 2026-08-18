"""Tests for ADK-native approval and current-task A2A HITL translation."""

from __future__ import annotations

import pytest
from a2a.types import Message, Part, Role, Task, TaskState, TaskStatus
from google.adk.a2a.converters.part_converter import (
    convert_a2a_part_to_genai_part,
    convert_genai_part_to_a2a_part,
)
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.genai import types as genai_types
from kagent.core.a2a import (
    AskUserRequest,
    AskUserResponse,
    HitlTool,
    NestedHitlRequest,
    ToolApproval,
    ToolApprovalRequest,
    ToolApprovalResponse,
    attach_hitl_extension,
    get_ask_user_request,
    get_tool_approval_request,
)

from kagent.adk._approval import make_approval_callback
from kagent.adk._hitl import (
    RemoteHitlState,
    build_hitl_status_message,
    build_resume_hitl_message,
)


class MockState(dict):
    pass


class MockEventActions:
    def __init__(self):
        self.requested_tool_confirmations: dict[str, ToolConfirmation] = {}


class MockToolContext:
    def __init__(self, tool_confirmation=None):
        self.state = MockState()
        self.function_call_id = "test_fc_id"
        self._event_actions = MockEventActions()
        self.tool_confirmation = tool_confirmation

    def request_confirmation(self, *, hint=None, payload=None):
        self._event_actions.requested_tool_confirmations[self.function_call_id] = ToolConfirmation(
            hint=hint,
            payload=payload,
        )


class MockBaseTool:
    def __init__(self, name: str):
        self.name = name


class TestMakeApprovalCallback:
    def test_allows_non_approval_tools(self):
        callback = make_approval_callback({"delete_file"})
        ctx = MockToolContext()
        assert callback(MockBaseTool("read_file"), {"path": "/tmp"}, ctx) is None
        assert not ctx._event_actions.requested_tool_confirmations

    def test_blocks_approval_tools_and_requests_confirmation(self):
        callback = make_approval_callback({"delete_file"})
        ctx = MockToolContext()
        result = callback(MockBaseTool("delete_file"), {"path": "/tmp"}, ctx)
        assert result == {"status": "confirmation_requested", "tool": "delete_file"}
        assert "delete_file" in ctx._event_actions.requested_tool_confirmations["test_fc_id"].hint

    def test_approved_confirmation_allows_execution(self):
        callback = make_approval_callback({"delete_file"})
        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        assert callback(MockBaseTool("delete_file"), {}, ctx) is None

    def test_rejected_confirmation_includes_reason(self):
        callback = make_approval_callback({"delete_file"})
        ctx = MockToolContext(
            tool_confirmation=ToolConfirmation(
                confirmed=False,
                payload={"rejection_reason": "Dangerous path"},
            )
        )
        assert callback(MockBaseTool("delete_file"), {}, ctx) == (
            "Tool call was rejected by user. Reason: Dangerous path"
        )

    def test_rejected_confirmation_without_reason(self):
        callback = make_approval_callback({"delete_file"})
        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=False))
        assert callback(MockBaseTool("delete_file"), {}, ctx) == "Tool call was rejected by user."


def _tool(identifier: str, name: str = "delete_file") -> HitlTool:
    return HitlTool(
        id=identifier,
        call_id=f"call-{identifier}",
        name=name,
        args={"path": f"/{identifier}"} if name != "ask_user" else {"questions": [{"question": "Where?"}]},
    )


def _stored_task(request: ToolApprovalRequest | AskUserRequest, *, state=TaskState.TASK_STATE_INPUT_REQUIRED) -> Task:
    message = Message(message_id="pause-message", role=Role.ROLE_AGENT, task_id="task-1", context_id="context-1")
    attach_hitl_extension(message, request)
    return Task(
        id="task-1",
        context_id="context-1",
        status=TaskStatus(state=state, message=message),
    )


def _incoming(response: ToolApprovalResponse | AskUserResponse) -> Message:
    message = Message(message_id="response-message", role=Role.ROLE_USER, task_id="task-1", context_id="context-1")
    return attach_hitl_extension(message, response)


def _confirmations(message: Message) -> dict[str, ToolConfirmation]:
    result = {}
    for part in message.parts:
        genai_part = convert_a2a_part_to_genai_part(part)
        assert genai_part is not None and not isinstance(genai_part, list)
        response = genai_part.function_response
        assert response is not None and response.id is not None
        result[response.id] = ToolConfirmation.from_response_dict(response.response or {})
    return result


def test_direct_tool_approvals_use_stored_task_not_session_history():
    task = _stored_task(ToolApprovalRequest(tools=[_tool("confirm-1"), _tool("confirm-2", "restart")]))
    incoming = _incoming(
        ToolApprovalResponse(
            approvals=[
                ToolApproval(id="confirm-1", approved=True),
                ToolApproval(id="confirm-2", approved=False, rejection_reason="not now"),
            ]
        )
    )

    confirmations = _confirmations(build_resume_hitl_message(task, incoming))

    assert confirmations["confirm-1"].confirmed is True
    assert confirmations["confirm-1"].payload is None
    assert confirmations["confirm-2"].confirmed is False
    assert confirmations["confirm-2"].payload == {"rejection_reason": "not now"}


@pytest.mark.parametrize(
    ("approvals", "error"),
    [
        ([ToolApproval(id="confirm-1", approved=True)] * 2, "duplicate ids"),
        ([ToolApproval(id="unknown", approved=True)], "missing id"),
        (
            [ToolApproval(id="confirm-1", approved=True), ToolApproval(id="unknown", approved=True)],
            "unknown ids",
        ),
    ],
)
def test_direct_tool_approval_rejects_invalid_correlation(approvals, error):
    task = _stored_task(ToolApprovalRequest(tools=[_tool("confirm-1")]))
    with pytest.raises(ValueError, match=error):
        build_resume_hitl_message(task, _incoming(ToolApprovalResponse(approvals=approvals)))


def test_direct_ask_user_response():
    request = AskUserRequest(id="ask-confirm", questions=[{"question": "Which namespace?"}])
    incoming = _incoming(
        AskUserResponse(id="ask-confirm", answers=[{"answer": ["default"]}]),
    )

    confirmation = _confirmations(build_resume_hitl_message(_stored_task(request), incoming))["ask-confirm"]

    assert confirmation.confirmed is True
    assert confirmation.payload == {"answers": [{"answer": ["default"]}]}


def test_nested_tool_approvals_restore_remote_state():
    parent = _tool("parent-confirm", "child_agent")
    children = [_tool("child-confirm-1"), _tool("child-confirm-2", "restart")]
    request = ToolApprovalRequest(
        hint="Child needs approval",
        tools=[parent],
        nested=NestedHitlRequest(
            subagent_name="child_agent",
            task_id="child-task",
            context_id="child-context",
            tools=children,
        ),
    )
    incoming = _incoming(
        ToolApprovalResponse(
            approvals=[
                ToolApproval(id="child-confirm-1", approved=True),
                ToolApproval(id="child-confirm-2", approved=False, rejection_reason="not now"),
            ]
        )
    )

    confirmation = _confirmations(build_resume_hitl_message(_stored_task(request), incoming))["parent-confirm"]
    remote_state = RemoteHitlState.model_validate(confirmation.payload)

    assert confirmation.confirmed is False
    assert remote_state.task_id == "child-task"
    assert remote_state.context_id == "child-context"
    assert isinstance(remote_state.hitl_request, ToolApprovalRequest)
    assert isinstance(remote_state.hitl_response, ToolApprovalResponse)
    assert [item.id for item in remote_state.hitl_response.approvals] == ["child-confirm-1", "child-confirm-2"]


def test_nested_ask_user_uses_child_response_and_parent_confirmation_ids():
    child = _tool("child-confirm", "ask_user")
    request = AskUserRequest(
        id="parent-confirm",
        questions=[{"question": "Which namespace?"}],
        nested=NestedHitlRequest(
            subagent_name="child_agent",
            task_id="child-task",
            context_id="child-context",
            tools=[child],
        ),
    )
    incoming = _incoming(
        AskUserResponse(id="child-confirm", answers=[{"answer": ["default"]}]),
    )

    confirmation = _confirmations(build_resume_hitl_message(_stored_task(request), incoming))["parent-confirm"]
    remote_state = RemoteHitlState.model_validate(confirmation.payload)

    assert confirmation.confirmed is True
    assert isinstance(remote_state.hitl_response, AskUserResponse)
    assert remote_state.hitl_response.id == "child-confirm"


def test_nested_ask_status_preserves_parent_confirmation_id():
    child_request = AskUserRequest(id="child-confirm", questions=[{"question": "Which namespace?"}])
    remote_state = RemoteHitlState(
        task_id="child-task",
        context_id="child-context",
        subagent_name="child_agent",
        hitl_request=child_request,
    )
    function_call = genai_types.Part(
        function_call=genai_types.FunctionCall(
            id="parent-confirm",
            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
            args={
                "originalFunctionCall": {
                    "id": "parent-call",
                    "name": "child_agent",
                    "args": {"request": "help"},
                },
                "toolConfirmation": {
                    "hint": "Child needs an answer",
                    "payload": remote_state.model_dump(exclude_none=True),
                },
            },
        )
    )
    converted = convert_genai_part_to_a2a_part(function_call)
    assert converted is not None and not isinstance(converted, list)

    status_message = build_hitl_status_message([converted], "task-1", "context-1", activated=True)
    public_request = get_ask_user_request(status_message)

    assert public_request is not None
    assert public_request.id == "parent-confirm"
    assert public_request.nested is not None
    assert public_request.nested.tools[0].id == "child-confirm"


def test_resume_rejects_non_input_required_task():
    task = _stored_task(
        ToolApprovalRequest(tools=[_tool("confirm-1")]),
        state=TaskState.TASK_STATE_COMPLETED,
    )
    with pytest.raises(ValueError, match="input-required"):
        build_resume_hitl_message(
            task,
            _incoming(ToolApprovalResponse(approvals=[ToolApproval(id="confirm-1", approved=True)])),
        )


def test_resume_rejects_input_required_task_without_public_hitl_request():
    task = Task(
        id="task-1",
        context_id="context-1",
        status=TaskStatus(
            state=TaskState.TASK_STATE_INPUT_REQUIRED,
            message=Message(message_id="pause", role=Role.ROLE_AGENT, parts=[]),
        ),
    )
    with pytest.raises(ValueError, match="no HITL request"):
        build_resume_hitl_message(
            task,
            _incoming(ToolApprovalResponse(approvals=[ToolApproval(id="confirm-1", approved=True)])),
        )


def _confirmation_part(confirmation_id: str, tool_name: str, args: dict, hint: str | None = None) -> Part:
    function_call = genai_types.Part(
        function_call=genai_types.FunctionCall(
            id=confirmation_id,
            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
            args={
                "originalFunctionCall": {"id": f"call-{confirmation_id}", "name": tool_name, "args": args},
                "toolConfirmation": {"hint": hint} if hint else {},
            },
        )
    )
    converted = convert_genai_part_to_a2a_part(function_call)
    assert converted is not None and not isinstance(converted, list)
    return converted


def _status_text(message: Message) -> str:
    return "".join(part.text for part in message.parts if part.HasField("text"))


def test_status_message_without_activation_names_the_tool():
    part = _confirmation_part("confirm-1", "delete_file", {"path": "/tmp/x"})

    status_message = build_hitl_status_message([part], "task-1", "context-1", activated=False)

    assert get_tool_approval_request(status_message) is None
    assert _status_text(status_message) == "Approval is required for tool(s): delete_file"


def test_status_message_without_activation_carries_the_question():
    part = _confirmation_part(
        "confirm-1",
        "ask_user",
        {"questions": [{"question": "Which namespace?"}]},
        hint="Which namespace?",
    )

    status_message = build_hitl_status_message([part], "task-1", "context-1", activated=False)

    assert get_ask_user_request(status_message) is None
    assert _status_text(status_message) == "Which namespace?"


def test_status_message_with_activation_keeps_the_same_text():
    part = _confirmation_part("confirm-1", "delete_file", {"path": "/tmp/x"})

    status_message = build_hitl_status_message([part], "task-1", "context-1", activated=True)
    request = get_tool_approval_request(status_message)

    assert request is not None
    assert request.tools[0].name == "delete_file"
    assert _status_text(status_message) == "Approval is required for tool(s): delete_file"


def test_status_message_without_confirmation_parts_stays_generic():
    status_message = build_hitl_status_message(
        [Part(text="waiting on authorization")], "task-1", "context-1", activated=True
    )

    assert get_tool_approval_request(status_message) is None
    assert _status_text(status_message) == "Human input is required before the agent can continue."
