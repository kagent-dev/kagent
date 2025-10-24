"""Tests for HITL utility functions."""

import pytest
from a2a.types import DataPart, Message, Part, TaskState, TextPart

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    ToolApprovalRequest,
    escape_markdown_backticks,
    extract_decision_from_message,
    format_tool_approval_text_parts,
    is_input_required_task,
)


def test_escape_markdown_backticks():
    """Test backtick escaping for all cases."""
    assert escape_markdown_backticks("foo`bar") == "foo\\`bar"
    assert escape_markdown_backticks("`code` and `more`") == "\\`code\\` and \\`more\\`"
    assert escape_markdown_backticks("plain text") == "plain text"
    assert escape_markdown_backticks("") == ""


def test_is_input_required_task():
    """Test is_input_required_task() for various states."""
    assert is_input_required_task(TaskState.input_required) is True
    assert is_input_required_task(TaskState.working) is False
    assert is_input_required_task(TaskState.completed) is False
    assert is_input_required_task(None) is False


def test_extract_decision_datapart():
    """Test DataPart decision extraction (priority 1)."""
    # Approve
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE}))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_APPROVE

    # Deny
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY}))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_DENY


def test_extract_decision_textpart():
    """Test TextPart keyword extraction (priority 2)."""
    # Approve keyword
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(TextPart(text="I have approved this action"))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_APPROVE

    # Deny keyword
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(TextPart(text="Request denied, do not proceed"))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_DENY

    # Case insensitive
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(TextPart(text="APPROVED"))],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_APPROVE


def test_extract_decision_priority():
    """Test DataPart takes priority over TextPart."""
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[
            Part(TextPart(text="approved")),  # Would detect as approve
            Part(DataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY})),  # But deny wins
        ],
    )
    assert extract_decision_from_message(message) == KAGENT_HITL_DECISION_TYPE_DENY


def test_extract_decision_edge_cases():
    """Test edge cases: empty message, no parts, no decision."""
    # Empty message
    assert extract_decision_from_message(None) is None

    # No parts
    message = Message(role="user", message_id="test", task_id="task1", context_id="ctx1", parts=[])
    assert extract_decision_from_message(message) is None

    # No decision found
    message = Message(
        role="user",
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(TextPart(text="This is just a comment"))],
    )
    assert extract_decision_from_message(message) is None


def test_format_tool_approval_text_parts():
    """Test formatting tool approval requests with all edge cases."""
    requests = [
        ToolApprovalRequest(name="search", args={"query": "test"}),
        ToolApprovalRequest(name="run`code`", args={"cmd": "echo `test`"}),
        ToolApprovalRequest(name="reset", args={}),
    ]
    parts = format_tool_approval_text_parts(requests)

    # Convert to text
    text_content = ""
    for p in parts:
        if hasattr(p, "root") and hasattr(p.root, "text"):
            text_content += p.root.text

    # Check structure and content
    assert "Approval Required" in text_content
    assert "search" in text_content
    assert "reset" in text_content
    # Check backticks are escaped
    assert "\\`" in text_content
