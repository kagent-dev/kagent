"""Google ADK adapters for the framework-neutral A2A HITL extension."""

from __future__ import annotations

import uuid
from typing import Annotated, Any

from a2a.types import Message, Part, Role, Task
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
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
    get_tool_approval_request,
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
            id=remote_state.hitl_request.id,
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
