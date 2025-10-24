"""Human-in-the-Loop (HITL) support for kagent executors.

This module provides types, utilities, and handlers for implementing
human-in-the-loop workflows in kagent agent executors using A2A protocol primitives.
"""

import logging
import uuid
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any, Literal

from a2a.server.events.event_queue import EventQueue
from a2a.server.tasks import TaskStore
from a2a.types import (
    DataPart,
    Message,
    Part,
    Role,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)

from ._consts import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL,
    KAGENT_HITL_RESUME_KEYWORDS_APPROVE,
    KAGENT_HITL_RESUME_KEYWORDS_DENY,
    get_kagent_metadata_key,
)

logger = logging.getLogger(__name__)

# Type definitions

DecisionType = Literal["approve", "deny", "reject"]
"""Type for user decisions in HITL workflows."""


@dataclass
class ToolApprovalRequest:
    """Generic structure for a tool call requiring approval.

    Any agent framework can map their tool calls to this structure.

    Attributes:
        name: The name of the tool/function being called
        args: Dictionary of arguments to pass to the tool
        id: Optional unique identifier for this specific tool call
    """

    name: str
    args: dict[str, Any]
    id: str | None = None


# Utility functions


def escape_markdown_backticks(text: str) -> str:
    """Escape backticks in text to prevent markdown formatting issues.

    Used when displaying code, tool names, or arguments in markdown-formatted
    approval messages.

    Args:
        text: Text that may contain backticks

    Returns:
        Text with all backticks escaped with backslash

    Examples:
        >>> escape_markdown_backticks("function `foo`")
        'function \\`foo\\`'
    """
    return str(text).replace("`", "\\`")


def is_input_required_task(task_state: TaskState | None) -> bool:
    """Check if task state indicates waiting for user input.

    Args:
        task_state: Current task state, or None if no task

    Returns:
        True if task is in input_required state
    """
    return task_state == TaskState.input_required


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


def extract_decision_from_text(text: str) -> DecisionType | None:
    """Extract decision from text using keyword matching.

    Searches for approval or denial keywords in the text (case-insensitive).
    Denial keywords take priority if both are present (to avoid accidental approval).

    Args:
        text: User input text

    Returns:
        "deny" if denial keywords found, "approve" if approval keywords found,
        None if no keywords found
    """
    text_lower = text.lower()

    # Check deny keywords first (safer - prevents accidental approval)
    if any(keyword in text_lower for keyword in KAGENT_HITL_RESUME_KEYWORDS_DENY):
        return KAGENT_HITL_DECISION_TYPE_DENY

    # Check approve keywords
    if any(keyword in text_lower for keyword in KAGENT_HITL_RESUME_KEYWORDS_APPROVE):
        return KAGENT_HITL_DECISION_TYPE_APPROVE

    return None


def extract_decision_from_message(message: Message | None) -> DecisionType | None:
    """Extract decision from A2A message using two-tier detection.

    Priority:
    1. Structured DataPart with decision_type field (most reliable)
    2. Keyword matching in TextPart (fallback for human input)

    DataPart is checked across all parts first before falling back to TextPart,
    ensuring structured decisions always take precedence.

    Args:
        message: A2A message from user

    Returns:
        Decision type if found, None otherwise
    """
    if not message or not message.parts:
        return None

    # Priority 1: Scan all parts for DataPart with decision (most reliable)
    for part in message.parts:
        # Access .root for RootModel union types
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, DataPart):
            decision = extract_decision_from_data_part(inner.data)
            if decision:
                return decision

    # Priority 2: Fallback to TextPart keyword matching
    for part in message.parts:
        if not hasattr(part, "root"):
            continue

        inner = part.root

        if isinstance(inner, TextPart):
            if inner.text and isinstance(inner.text, str):
                decision = extract_decision_from_text(inner.text)
                if decision:
                    return decision

    return None


def format_tool_approval_text_parts(
    action_requests: list[ToolApprovalRequest],
) -> list[Part]:
    """Format tool approval requests as human-readable TextParts.

    Creates a formatted approval message listing all tools and their arguments
    with proper markdown escaping to prevent rendering issues.

    Args:
        action_requests: List of tool approval request objects

    Returns:
        List of Part objects containing formatted approval message
    """
    parts = []

    # Add header
    parts.append(Part(TextPart(text="**Approval Required**\n\n")))
    parts.append(Part(TextPart(text="The following actions require your approval:\n\n")))

    # List each action
    for action in action_requests:
        tool_name = action.name
        tool_args = action.args

        # Escape backticks to prevent markdown breaking
        escaped_tool_name = escape_markdown_backticks(tool_name)
        parts.append(Part(TextPart(text=f"**Tool**: `{escaped_tool_name}`\n")))
        parts.append(Part(TextPart(text="**Arguments**:\n")))

        for key, value in tool_args.items():
            escaped_key = escape_markdown_backticks(key)
            escaped_value = escape_markdown_backticks(value)
            parts.append(Part(TextPart(text=f"  • {escaped_key}: `{escaped_value}`\n")))

        parts.append(Part(TextPart(text="\n")))

    return parts


# High-level handlers


async def handle_tool_approval_interrupt(
    action_requests: list[ToolApprovalRequest],
    task_id: str,
    context_id: str,
    event_queue: EventQueue,
    task_store: TaskStore,
    app_name: str | None = None,
    review_configs: list[dict[str, Any]] | None = None,
) -> None:
    """Send input_required event for tool approval.

    This is a framework-agnostic handler that any executor can call when
    it needs user approval for tool calls. It formats an approval message,
    sends an input_required event, and waits for the task to be saved.

    Args:
        action_requests: List of tool calls requiring approval
        task_id: A2A task ID
        context_id: A2A context ID
        event_queue: Event queue for publishing events
        task_store: Task store for synchronization
        app_name: Optional application name for metadata
        review_configs: Optional framework-specific review configurations

    Raises:
        TimeoutError: If task save doesn't complete within 5 seconds (logged as warning)
    """
    # Build human-readable message
    text_parts = format_tool_approval_text_parts(action_requests)

    # Build structured DataPart for machine processing (client can parse this)
    interrupt_data = {
        "interrupt_type": KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL,
        "action_requests": [{"name": req.name, "args": req.args, "id": req.id} for req in action_requests],
    }

    if review_configs:
        interrupt_data["review_configs"] = review_configs

    data_part = Part(
        DataPart(
            data=interrupt_data,
            metadata={get_kagent_metadata_key("type"): "interrupt_data"},
        )
    )

    # Combine message parts
    message_parts = text_parts + [data_part]

    # Build event metadata
    event_metadata = {"interrupt_type": KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL}
    if app_name:
        event_metadata["app_name"] = app_name

    # Send input_required event
    await event_queue.enqueue_event(
        TaskStatusUpdateEvent(
            task_id=task_id,
            status=TaskStatus(
                state=TaskState.input_required,
                timestamp=datetime.now(UTC).isoformat(),
                message=Message(
                    message_id=str(uuid.uuid4()),
                    role=Role.agent,
                    parts=message_parts,
                ),
            ),
            context_id=context_id,
            final=False,  # Not final - waiting for user input
            metadata=event_metadata,
        )
    )

    logger.info(f"Interrupt detected, sent input_required event for task {task_id} with {len(action_requests)} actions")

    # Wait for the event consumer to persist the task (event-based sync)
    # This prevents race condition where approval arrives before task is saved
    try:
        await task_store.wait_for_save(task_id, timeout=5.0)
    except TimeoutError:
        logger.warning("Task save event timeout, proceeding anyway")
