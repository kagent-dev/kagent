"""Tests for HITL constants."""

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL,
    KAGENT_HITL_RESUME_KEYWORDS_APPROVE,
    KAGENT_HITL_RESUME_KEYWORDS_DENY,
)


def test_hitl_constants():
    """Test all HITL constants are defined with expected values."""
    # Interrupt types
    assert KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL == "tool_approval"

    # Decision types
    assert KAGENT_HITL_DECISION_TYPE_KEY == "decision_type"
    assert KAGENT_HITL_DECISION_TYPE_APPROVE == "approve"
    assert KAGENT_HITL_DECISION_TYPE_DENY == "deny"
    assert KAGENT_HITL_DECISION_TYPE_REJECT == "reject"

    # Resume keywords
    assert "approved" in KAGENT_HITL_RESUME_KEYWORDS_APPROVE
    assert "proceed" in KAGENT_HITL_RESUME_KEYWORDS_APPROVE
    assert "denied" in KAGENT_HITL_RESUME_KEYWORDS_DENY
    assert "cancel" in KAGENT_HITL_RESUME_KEYWORDS_DENY
