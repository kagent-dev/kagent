"""A2A Human-in-the-Loop extension primitives.

The extension is an A2A Message extension. ADK confirmation events remain an
executor implementation detail and are never the protocol shape sent to clients.
"""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, Literal

from a2a.types import AgentCard, Message
from google.protobuf.json_format import MessageToDict, ParseDict
from pydantic import BaseModel, ConfigDict, Field, ValidationError

HITL_EXTENSION_URI = "https://kagent.dev/extensions/hitl/v1"
HITL_EXTENSION_HEADER = "A2A-Extensions"
HITL_TYPE_TOOL_APPROVAL_REQUEST = "tool_approval_request"
HITL_TYPE_ASK_USER_REQUEST = "ask_user_request"
HITL_TYPE_TOOL_APPROVAL_RESPONSE = "tool_approval_response"
HITL_TYPE_ASK_USER_RESPONSE = "ask_user_response"


class HitlTool(BaseModel):
    model_config = ConfigDict(extra="forbid")
    id: str = Field(min_length=1)
    call_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    args: dict[str, Any] = Field(default_factory=dict)


class NestedHitlRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    subagent_name: str | None = None
    task_id: str | None = None
    context_id: str | None = None
    tools: list[HitlTool] = Field(min_length=1)


class ToolApprovalRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: Literal["tool_approval_request"] = HITL_TYPE_TOOL_APPROVAL_REQUEST
    hint: str | None = None
    tools: list[HitlTool] = Field(min_length=1)
    nested: NestedHitlRequest | None = None


class AskUserRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: Literal["ask_user_request"] = HITL_TYPE_ASK_USER_REQUEST
    id: str = Field(min_length=1)
    questions: list[dict[str, Any]]
    nested: NestedHitlRequest | None = None


class ToolApproval(BaseModel):
    model_config = ConfigDict(extra="forbid")
    id: str = Field(min_length=1)
    approved: bool
    rejection_reason: str | None = None


class ToolApprovalResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: Literal["tool_approval_response"] = HITL_TYPE_TOOL_APPROVAL_RESPONSE
    approvals: list[ToolApproval] = Field(min_length=1)


class AskUserResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: Literal["ask_user_response"] = HITL_TYPE_ASK_USER_RESPONSE
    id: str = Field(min_length=1)
    answers: list[dict[str, list[str]]] | None = None


def get_tool_approval_request(message: Message | None) -> ToolApprovalRequest | None:
    """Parse a tool approval request from an A2A Message."""
    payload = get_hitl_payload(message)
    if payload is None or payload.get("type") != HITL_TYPE_TOOL_APPROVAL_REQUEST:
        return None
    try:
        return ToolApprovalRequest.model_validate(payload)
    except ValidationError:
        return None


def get_ask_user_request(message: Message | None) -> AskUserRequest | None:
    """Parse an ask-user request from an A2A Message."""
    payload = get_hitl_payload(message)
    if payload is None or payload.get("type") != HITL_TYPE_ASK_USER_REQUEST:
        return None
    try:
        return AskUserRequest.model_validate(payload)
    except ValidationError:
        return None


def get_tool_approval_response(message: Message | None) -> ToolApprovalResponse | None:
    """Parse a tool approval response from an A2A Message."""
    payload = get_hitl_payload(message)
    if payload is None or payload.get("type") != HITL_TYPE_TOOL_APPROVAL_RESPONSE:
        return None
    try:
        return ToolApprovalResponse.model_validate(payload)
    except ValidationError:
        return None


def get_ask_user_response(message: Message | None) -> AskUserResponse | None:
    """Parse an ask-user response from an A2A Message."""
    payload = get_hitl_payload(message)
    if payload is None or payload.get("type") != HITL_TYPE_ASK_USER_RESPONSE:
        return None
    try:
        return AskUserResponse.model_validate(payload)
    except ValidationError:
        return None


def hitl_activated(headers: Mapping[str, Any] | None) -> bool:
    if not headers:
        return False
    value = next((v for k, v in headers.items() if k.lower() == HITL_EXTENSION_HEADER.lower()), "")
    values = value if isinstance(value, (list, tuple)) else [value]
    return any(HITL_EXTENSION_URI in {item.strip() for item in str(v).split(",")} for v in values)


def get_hitl_payload(message: Message | None) -> dict[str, Any] | None:
    """Get the HITL payload from an A2A Message."""
    if message is None or HITL_EXTENSION_URI not in message.extensions:
        return None
    metadata = MessageToDict(message.metadata) if message.HasField("metadata") else {}
    payload = metadata.get(HITL_EXTENSION_URI)
    return payload if isinstance(payload, dict) and isinstance(payload.get("type"), str) else None


def attach_hitl_extension(message: Message, payload: dict[str, Any] | BaseModel) -> Message:
    """Attach a HITL extension to an A2A Message."""
    data = payload.model_dump(exclude_none=True) if isinstance(payload, BaseModel) else payload
    metadata = MessageToDict(message.metadata) if message.HasField("metadata") else {}
    metadata[HITL_EXTENSION_URI] = data
    ParseDict(metadata, message.metadata)
    if HITL_EXTENSION_URI not in message.extensions:
        message.extensions.append(HITL_EXTENSION_URI)
    return message


def hitl_agent_extension() -> dict[str, Any]:
    """Get the HITL agent extension."""
    return {
        "uri": HITL_EXTENSION_URI,
        "description": "Human in the loop for tool approval, ask user, and nested subagents",
        "required": False,
    }


def attach_hitl_agent_extension(card: AgentCard) -> AgentCard:
    """Declare the optional extension on a protobuf AgentCard without replacing others."""
    if any(extension.uri == HITL_EXTENSION_URI for extension in card.capabilities.extensions):
        return card
    ParseDict(hitl_agent_extension(), card.capabilities.extensions.add())
    return card
