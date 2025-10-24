"""HITL interrupt/approval Pydantic models

These models define our custom protocol for tool approval interrupts.
They extend the standard A2A protocol with structured approval data.
"""

from typing import Any, Literal

from pydantic import BaseModel, Field


class ActionRequest(BaseModel):
    """Tool execution request requiring human approval.

    Sent by agents when they need permission to execute a tool.
    """

    name: str = Field(..., description="Tool name (e.g., 'kubectl_apply')")
    args: dict[str, Any] = Field(default_factory=dict, description="Tool arguments")

    class Config:
        json_schema_extra = {
            "example": {"name": "kubectl_apply", "args": {"namespace": "default", "manifest": "deployment.yaml"}}
        }


class ReviewConfig(BaseModel):
    """Per-tool approval configuration.

    Defines which decisions are allowed for each tool.
    Currently passed through but not used in UI.
    """

    tool_name: str
    allowed_decisions: list[str] = Field(default_factory=lambda: ["approve", "deny"])


class InterruptData(BaseModel):
    """Interrupt data for tool approval requests.

    Embedded in A2A DataPart when agent needs human approval.
    The agent sends this in a message part with kind="data".
    """

    interrupt_type: Literal["tool_approval"] = Field(..., description="Must be 'tool_approval' for our protocol")
    action_requests: list[ActionRequest] = Field(..., description="List of tools requiring approval")
    review_configs: list[ReviewConfig] = Field(default_factory=list, description="Optional per-tool configurations")

    class Config:
        json_schema_extra = {
            "example": {
                "interrupt_type": "tool_approval",
                "action_requests": [
                    {"name": "kubectl_delete", "args": {"namespace": "prod", "resource": "deployment/api"}}
                ],
                "review_configs": [],
            }
        }
