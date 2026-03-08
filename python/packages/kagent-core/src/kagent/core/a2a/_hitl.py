"""Human-in-the-Loop (HITL) support for kagent executors.

This module provides types, utilities, and handlers for implementing
human-in-the-loop workflows in kagent agent executors using A2A protocol primitives.
"""

import logging
from typing import Literal

from a2a.types import (
    DataPart,
    Message,
)

from ._consts import (
    KAGENT_ASK_USER_ANSWERS_KEY,
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    KAGENT_HITL_DECISIONS_KEY,
    KAGENT_HITL_REJECTION_REASONS_KEY,
)

logger = logging.getLogger(__name__)

# Type definitions

DecisionType = Literal["approve", "deny", "reject", "batch"]
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
        KAGENT_HITL_DECISION_TYPE_DENY,
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
    individual decisions ("approve" or "deny").

    Example DataPart data::

        {"decision_type": "batch", "decisions": {"call_abc123": "approve", "call_def456": "deny"}}

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
                        KAGENT_HITL_DECISION_TYPE_DENY,
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
            elif decision in (KAGENT_HITL_DECISION_TYPE_DENY, KAGENT_HITL_DECISION_TYPE_REJECT):
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
