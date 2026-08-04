"""Google ADK adapters for the framework-neutral A2A HITL extension."""

from __future__ import annotations

import uuid
from typing import Annotated, Any

from a2a.types import Message, Part, Role, Task, TaskState
from google.adk.a2a.converters.part_converter import convert_genai_part_to_a2a_part
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    AskUserRequest,
    AskUserResponse,
    HitlTool,
    NestedHitlRequest,
    ToolApprovalRequest,
    ToolApprovalResponse,
    attach_hitl_extension,
    get_ask_user_request,
    get_ask_user_response,
    get_tool_approval_request,
    get_tool_approval_response,
)
from pydantic import BaseModel, ConfigDict, Field, ValidationError

HitlRequest = Annotated[ToolApprovalRequest | AskUserRequest, Field(discriminator="type")]
HitlResponse = Annotated[ToolApprovalResponse | AskUserResponse, Field(discriminator="type")]


class RemoteHitlState(BaseModel):
    """State retained by an ADK remote tool while its child task is paused."""

    model_config = ConfigDict(extra="forbid")

    task_id: str
    context_id: str | None = None
    subagent_name: str
    hitl_request: HitlRequest
    hitl_response: HitlResponse | None = None


def extract_hitl_request_from_task(task: Task) -> ToolApprovalRequest | AskUserRequest | None:
    """Return the validated public HITL request on a child task."""
    if not task.status or not task.status.message:
        return None
    tool_request = get_tool_approval_request(task.status.message)
    if tool_request is not None:
        return tool_request
    return get_ask_user_request(task.status.message)


def build_remote_hitl_state(task: Task, subagent_name: str) -> RemoteHitlState | None:
    """Capture a child task's public request as ADK confirmation payload state."""
    request = extract_hitl_request_from_task(task)
    if request is None:
        return None
    return RemoteHitlState(
        task_id=task.id,
        context_id=task.context_id,
        subagent_name=subagent_name,
        hitl_request=request,
    )


def get_remote_hitl_state(payload: dict[str, Any] | None) -> RemoteHitlState | None:
    """Parse state created for a paused remote A2A tool."""
    if not payload or "hitl_request" not in payload:
        return None
    try:
        return RemoteHitlState.model_validate(payload)
    except ValidationError:
        return None


def visible_tools(request: ToolApprovalRequest | AskUserRequest) -> list[HitlTool]:
    """Return the tool operations a human acts on for a public HITL request."""
    if request.nested is not None:
        return request.nested.tools
    if isinstance(request, ToolApprovalRequest):
        return request.tools
    return [
        HitlTool(
            id=request.id,
            call_id=request.id,
            name="ask_user",
            args={"questions": request.questions},
        )
    ]


def remote_hitl_hint(state: RemoteHitlState) -> str:
    """Build the parent confirmation hint for a paused child task."""
    names = [tool.name for tool in visible_tools(state.hitl_request)]
    if names:
        return f"Remote agent '{state.subagent_name}' requires approval for tool(s): {', '.join(names)}"
    return f"Remote agent '{state.subagent_name}' requires human input before continuing."


def _tool_from_confirmation_data(data: dict[str, Any]) -> HitlTool:
    original = data.get("args", {}).get("originalFunctionCall", {})
    return HitlTool(
        id=str(data.get("id") or ""),
        call_id=str(original.get("id") or data.get("id") or ""),
        name=str(original.get("name") or "tool"),
        args=original.get("args") if isinstance(original.get("args"), dict) else {},
    )


def build_hitl_status_message(parts: list[Part], task_id: str, context_id: str, activated: bool) -> Message:
    """Translate ADK confirmation parts into a public A2A HITL request."""
    message = Message(message_id=str(uuid.uuid4()), role=Role.ROLE_AGENT, task_id=task_id, context_id=context_id)
    default_hint = "Human input is required before the agent can continue."
    if not activated:
        message.parts.append(Part(text=default_hint))
        return message

    tools: list[HitlTool] = []
    hint: str | None = None
    remote_state: RemoteHitlState | None = None
    for part in parts:
        if not part.HasField("data"):
            continue
        data = MessageToDict(part.data)
        if data.get("name") != REQUEST_CONFIRMATION_FUNCTION_CALL_NAME:
            continue
        tools.append(_tool_from_confirmation_data(data))
        tool_confirmation = data.get("args", {}).get("toolConfirmation", {})
        hint = hint or tool_confirmation.get("hint")
        candidate = get_remote_hitl_state(tool_confirmation.get("payload"))
        if candidate is not None:
            remote_state = candidate

    # Auth-required parts can share the long-running path without being HITL
    # confirmations. Leave those messages unextended.
    if not tools:
        message.parts.append(Part(text=hint or default_hint))
        return message

    nested: NestedHitlRequest | None = None
    if remote_state is not None:
        nested = NestedHitlRequest(
            subagent_name=remote_state.subagent_name,
            task_id=remote_state.task_id,
            context_id=remote_state.context_id,
            tools=visible_tools(remote_state.hitl_request),
        )

    if remote_state is not None and isinstance(remote_state.hitl_request, AskUserRequest):
        request: ToolApprovalRequest | AskUserRequest = AskUserRequest(
            # The top-level ID correlates the response to the parent's
            # adk_request_confirmation call. The child correlation remains in
            # nested.tools and is the ID returned by the UI.
            id=tools[0].id,
            questions=remote_state.hitl_request.questions,
            nested=nested,
        )
    elif len(tools) == 1 and tools[0].name == "ask_user":
        request = AskUserRequest(
            id=tools[0].id,
            questions=tools[0].args.get("questions") or [],
        )
    else:
        request = ToolApprovalRequest(hint=hint, tools=tools, nested=nested)

    attach_hitl_extension(message, request)
    message.parts.append(Part(text=hint or default_hint))
    return message


def _confirmation_response_part(call_id: str, confirmation: ToolConfirmation) -> Part:
    """Build an upstream-compatible A2A function-response part."""
    genai_part = genai_types.Part(
        function_response=genai_types.FunctionResponse(
            name=REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
            id=call_id,
            response={"response": confirmation.model_dump_json(by_alias=True, exclude_none=True)},
        )
    )
    converted = convert_genai_part_to_a2a_part(genai_part)
    if converted is None or isinstance(converted, list):
        raise ValueError("ADK did not produce one A2A confirmation response part")
    return converted


def _approval_map(response: ToolApprovalResponse) -> dict[str, Any]:
    approvals = {approval.id: approval for approval in response.approvals}
    if len(approvals) != len(response.approvals):
        raise ValueError("Tool approval response contains duplicate ids")
    return approvals


def _build_nested_resume(
    request: ToolApprovalRequest | AskUserRequest,
    incoming: Message,
) -> list[Part]:
    nested = request.nested
    if nested is None:
        raise ValueError("Nested HITL request is missing nested state")
    if not nested.task_id or not nested.subagent_name:
        raise ValueError("Nested HITL request is missing subagent task correlation")

    if isinstance(request, AskUserRequest):
        if len(nested.tools) != 1 or nested.tools[0].name != "ask_user":
            raise ValueError("Nested ask_user request must contain exactly one ask_user tool")
        response = get_ask_user_response(incoming)
        child_tool = nested.tools[0]
        if response is None or response.id != child_tool.id:
            raise ValueError("Nested ask_user response has invalid correlation")
        if not response.answers:
            raise ValueError("Nested ask_user response contains no answers")
        child_request: ToolApprovalRequest | AskUserRequest = AskUserRequest(
            id=child_tool.id,
            questions=request.questions,
        )
        child_response: ToolApprovalResponse | AskUserResponse = response
        outer_confirmation_id = request.id
        confirmed = True
    else:
        if len(request.tools) != 1:
            raise ValueError("Nested HITL request must contain exactly one parent tool")
        response = get_tool_approval_response(incoming)
        if response is None:
            raise ValueError("Nested tool approval request requires a tool approval response")
        approvals = _approval_map(response)
        nested_approvals = []
        for tool in nested.tools:
            approval = approvals.pop(tool.id, None)
            if approval is None:
                raise ValueError(f"Nested tool approval response is missing id: {tool.id}")
            nested_approvals.append(approval)
        if approvals:
            raise ValueError(f"Nested tool approval response contains unknown ids: {', '.join(sorted(approvals))}")
        child_request = ToolApprovalRequest(hint=request.hint, tools=nested.tools)
        child_response = ToolApprovalResponse(approvals=nested_approvals)
        outer_confirmation_id = request.tools[0].id
        confirmed = all(approval.approved for approval in nested_approvals)

    remote_state = RemoteHitlState(
        task_id=nested.task_id,
        context_id=nested.context_id,
        subagent_name=nested.subagent_name,
        hitl_request=child_request,
        hitl_response=child_response,
    )
    confirmation = ToolConfirmation(
        confirmed=confirmed,
        payload=remote_state.model_dump(exclude_none=True),
    )
    return [_confirmation_response_part(outer_confirmation_id, confirmation)]


def build_resume_hitl_message(stored_task: Task, incoming: Message) -> Message:
    """Translate a public HITL response using the stored A2A current task.

    Upstream ADK remains responsible for validating the reconstructed
    confirmation against its session history. Kagent only restores the ADK
    function-response wire shape from the public extension stored on the task.
    """
    if stored_task.status.state != TaskState.TASK_STATE_INPUT_REQUIRED:
        raise ValueError("HITL decision requires a stored input-required task")

    stored_message = stored_task.status.message
    request: ToolApprovalRequest | AskUserRequest | None = get_tool_approval_request(stored_message)
    if request is None:
        request = get_ask_user_request(stored_message)
    if request is None:
        raise ValueError("Stored input-required task has no HITL request")

    if request.nested is not None:
        parts = _build_nested_resume(request, incoming)
    elif isinstance(request, AskUserRequest):
        response = get_ask_user_response(incoming)
        if response is None or response.id != request.id:
            raise ValueError("ask_user response has invalid correlation")
        if not response.answers:
            raise ValueError("ask_user response contains no answers")
        parts = [
            _confirmation_response_part(
                request.id,
                ToolConfirmation(confirmed=True, payload={"answers": response.answers}),
            )
        ]
    else:
        response = get_tool_approval_response(incoming)
        if response is None:
            raise ValueError("Tool approval request requires a tool approval response")
        approvals = _approval_map(response)
        parts = []
        for tool in request.tools:
            approval = approvals.pop(tool.id, None)
            if approval is None:
                raise ValueError(f"Tool approval response is missing id: {tool.id}")
            payload = None
            if not approval.approved and approval.rejection_reason:
                payload = {"rejection_reason": approval.rejection_reason}
            parts.append(
                _confirmation_response_part(
                    tool.id,
                    ToolConfirmation(confirmed=approval.approved, payload=payload),
                )
            )
        if approvals:
            raise ValueError(f"Tool approval response contains unknown ids: {', '.join(sorted(approvals))}")

    return Message(
        message_id=str(uuid.uuid4()),
        role=Role.ROLE_USER,
        task_id=incoming.task_id,
        context_id=incoming.context_id,
        parts=parts,
    )
