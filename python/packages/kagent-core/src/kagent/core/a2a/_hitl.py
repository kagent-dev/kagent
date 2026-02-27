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
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
)

logger = logging.getLogger(__name__)

# Type definitions

DecisionType = Literal["approve", "deny", "reject"]
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
