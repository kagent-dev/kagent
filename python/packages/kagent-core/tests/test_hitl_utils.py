"""Tests for HITL utility functions."""

from a2a.types import DataPart, Message, Part, Role

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    extract_decision_from_message,
)


def test_extract_decision_datapart():
    """Test DataPart decision extraction (priority 1)."""
    # Approve
    message = Message(
        role=Role.user,
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE}))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_APPROVE

    # Deny
    message = Message(
        role=Role.user,
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY}))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_DENY


def test_extract_decision_edge_cases():
    """Test edge cases: empty message, no parts, no decision."""
    # Empty message
    assert extract_decision_from_message(None) is None

    # No parts
    message = Message(role=Role.user, message_id="test", task_id="task1", context_id="ctx1", parts=[])
    assert extract_decision_from_message(message) is None

    # No decision DataPart found
    message = Message(
        role=Role.user,
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data={"some_other_key": "value"}))],
    )
    assert extract_decision_from_message(message) is None
