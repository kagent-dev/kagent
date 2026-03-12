"""Human-in-the-Loop (HITL) support for kagent executors.

This module provides types, utilities, and handlers for implementing
human-in-the-loop workflows in kagent agent executors using A2A protocol primitives.
"""

from __future__ import annotations

import logging
from typing import Any, Literal

from a2a.types import (
    DataPart,
    Message,
    Task,
)
from pydantic import BaseModel, ConfigDict, Field

from ._consts import (
    A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    KAGENT_ASK_USER_ANSWERS_KEY,
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    KAGENT_HITL_DECISIONS_KEY,
    KAGENT_HITL_REJECTION_REASONS_KEY,
    read_metadata_value,
)

logger = logging.getLogger(__name__)


class OriginalFunctionCall(BaseModel):
    """The original tool function call that requires human approval.

    Mirrors the shape produced by ``genai_types.FunctionCall.model_dump()``
    inside the ``adk_request_confirmation`` event.
    """

    model_config = ConfigDict(populate_by_name=True)

    name: str
    """Tool name (e.g. ``"delete_file"``)."""

    args: dict[str, Any] = Field(default_factory=dict)
    """Tool call arguments."""

    id: str | None = None
    """Original function-call ID assigned by the LLM."""


class HitlPartInfo(BaseModel):
    """Structured representation of one ``adk_request_confirmation`` DataPart.

    Each instance corresponds to a single tool call that is waiting for
    human approval in the subagent.  The upstream ADK serialises this as:

    ```json
    {
        "name": "adk_request_confirmation",
        "id": "<confirm_fc_id>",
        "args": {
            "originalFunctionCall": {"name": ..., "args": ..., "id": ...},
            "toolConfirmation": {"hint": ..., "confirmed": false, ...}
        }
    }
    ```

    ``toolConfirmation`` is intentionally omitted from this model because
    consumers only need the original function call details.  The full dict
    is still preserved in the A2A DataPart if needed.
    """

    model_config = ConfigDict(populate_by_name=True)

    name: str
    """Always ``"adk_request_confirmation"``."""

    id: str | None = None
    """The confirmation function-call ID (distinct from the original FC ID)."""

    original_function_call: OriginalFunctionCall = Field(alias="originalFunctionCall")
    """The original tool call that needs approval."""

    @classmethod
    def from_data_part_data(cls, data: dict[str, Any]) -> HitlPartInfo:
        """Construct from the raw ``DataPart.data`` dict.

        The dict has ``args.originalFunctionCall`` nested one level deep.
        This factory flattens it into the model fields.
        """
        args = data.get("args", {})
        return cls(
            name=data.get("name", "adk_request_confirmation"),
            id=data.get("id"),
            originalFunctionCall=OriginalFunctionCall(**args.get("originalFunctionCall", {})),
        )

    @property
    def tool_name(self) -> str | None:
        """Convenience accessor for the inner tool name."""
        return self.original_function_call.name

    @property
    def tool_call_id(self) -> str | None:
        """Convenience accessor for the original function-call ID."""
        return self.original_function_call.id


DecisionType = Literal["approve", "reject", "batch"]
"""Type for user decisions in HITL workflows."""


def extract_decision_from_data_part(data: dict) -> DecisionType | None:
    """Extract decision type from structured DataPart.

    Looks for the decision_type key in the data dictionary and validates
    it's a known decision value.

    Args:
        data: DataPart.data dictionary

    Returns:
        Decision type if found and valid, None otherwise
    """
    decision = data.get(KAGENT_HITL_DECISION_TYPE_KEY)
    if decision in (
        KAGENT_HITL_DECISION_TYPE_APPROVE,
        KAGENT_HITL_DECISION_TYPE_REJECT,
        KAGENT_HITL_DECISION_TYPE_BATCH,
    ):
        return decision
    return None


def extract_decision_from_message(message: Message | None) -> DecisionType | None:
    """Extract decision from A2A message.

    Client frontend sends a structured DataPart with a decision_type
    key to indicate tool approval/denial.

    Args:
        message: A2A message from user

    Returns:
        Decision type if found, None otherwise
    """
    if not message or not message.parts:
        return None

    for part in message.parts:
        # Access .root for RootModel union types
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, DataPart):
            decision = extract_decision_from_data_part(inner.data)
            if decision:
                return decision

    return None


def extract_batch_decisions_from_message(message: Message | None) -> dict[str, DecisionType] | None:
    """Extract per-tool batch decisions from A2A message.

    When the UI sends a batch decision (decision_type="batch"), the DataPart
    also contains a ``decisions`` dict mapping original tool call IDs to their
    individual decisions ("approve" or "reject").

    Example DataPart data::

        {"decision_type": "batch", "decisions": {"call_abc123": "approve", "call_def456": "reject"}}

    Args:
        message: A2A message from user

    Returns:
        Dict mapping original tool call IDs to decision types, or None
        if no batch decisions found.
    """
    if not message or not message.parts:
        return None

    for part in message.parts:
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, DataPart):
            data = inner.data
            if data.get(KAGENT_HITL_DECISION_TYPE_KEY) != KAGENT_HITL_DECISION_TYPE_BATCH:
                continue

            decisions = data.get(KAGENT_HITL_DECISIONS_KEY)
            if isinstance(decisions, dict):
                # Filter out invalid decisions
                filtered: dict[str, DecisionType] = {}
                for call_id, decision in decisions.items():
                    # Ensure key type and decision value are valid
                    if not isinstance(call_id, str):
                        logger.warning("Ignoring HITL batch decision with non-string key: %r", call_id)
                        continue
                    if decision in (
                        KAGENT_HITL_DECISION_TYPE_APPROVE,
                        KAGENT_HITL_DECISION_TYPE_REJECT,
                    ):
                        filtered[call_id] = decision
                    else:
                        logger.warning(
                            "Ignoring HITL batch decision with invalid value %r for call_id %r",
                            decision,
                            call_id,
                        )
                return filtered or None

    return None


def extract_rejection_reasons_from_message(message: Message | None) -> dict[str, str] | None:
    """Extract per-tool rejection reasons from A2A message.

    For uniform denials, the reason is extracted from the top-level
    ``rejection_reason`` key and returned mapped to the sentinel key ``"*"``.
    For batch denials, reasons are extracted from the ``rejection_reasons``
    dict (mapping original tool call IDs → reason strings).

    Args:
        message: A2A message from user

    Returns:
        Dict mapping original tool call IDs (or ``"*"`` for uniform) to
        reason strings, or None if no reasons found.
    """
    if not message or not message.parts:
        return None

    for part in message.parts:
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, DataPart):
            data = inner.data
            decision = data.get(KAGENT_HITL_DECISION_TYPE_KEY)

            if decision == KAGENT_HITL_DECISION_TYPE_BATCH:
                reasons = data.get(KAGENT_HITL_REJECTION_REASONS_KEY)
                if isinstance(reasons, dict):
                    filtered: dict[str, str] = {}
                    for call_id, reason in reasons.items():
                        if isinstance(call_id, str) and isinstance(reason, str) and reason:
                            filtered[call_id] = reason
                    return filtered or None
            elif decision == KAGENT_HITL_DECISION_TYPE_REJECT:
                reason = data.get("rejection_reason")
                if isinstance(reason, str) and reason:
                    return {"*": reason}

    return None


def extract_ask_user_answers_from_message(message: Message | None) -> list[dict] | None:
    """Extract ask-user answers from A2A message.

    When the UI sends an ask-user response, the DataPart contains an
    ``ask_user_answers`` list of ``{answer: [...]}`` dicts.

    Args:
        message: A2A message from user

    Returns:
        List of answer dicts, or None if this is not an ask-user response.
    """
    if not message or not message.parts:
        return None

    for part in message.parts:
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, DataPart):
            data = inner.data
            answers = data.get(KAGENT_ASK_USER_ANSWERS_KEY)
            if isinstance(answers, list):
                return answers

    return None


def extract_hitl_info_from_task(task: Task) -> list[HitlPartInfo] | None:
    """Extract HITL info from an ``input_required`` A2A Task's status message.

    Scans the task's status message parts for ``adk_request_confirmation``
    DataParts (identified by metadata ``type: "function_call"`` and
    ``is_long_running: true``) and returns them as typed
    :class:`HitlPartInfo` instances.

    Args:
        task: An A2A ``Task`` (typically with ``TaskState.input_required``).

    Returns:
        List of :class:`HitlPartInfo` objects, or ``None`` if no HITL parts
        are found.
    """
    if not task.status or not task.status.message or not task.status.message.parts:
        return None

    hitl_parts: list[HitlPartInfo] = []
    for part in task.status.message.parts:
        root = part.root if hasattr(part, "root") else part
        if not isinstance(root, DataPart) or not root.metadata:
            continue
        part_type = read_metadata_value(root.metadata, A2A_DATA_PART_METADATA_TYPE_KEY)
        is_long_running = read_metadata_value(root.metadata, A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY)
        if part_type == A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL and is_long_running is True:
            hitl_parts.append(HitlPartInfo.from_data_part_data(root.data))

    return hitl_parts or None
