"""Slack Block Kit formatting"""

import re
from typing import Optional, Any
from datetime import datetime
from ..constants import SLACK_BLOCK_LIMIT, EMOJI_ROBOT, EMOJI_CLOCK


def needs_approval(response_text: str) -> bool:
    """
    Detect if agent response needs human approval

    Args:
        response_text: Agent's response text

    Returns:
        True if approval is needed
    """
    approval_patterns = [
        r"should I",
        r"do you want me to",
        r"shall I",
        r"would you like me to",
        r"may I",
        r"confirm",
        r"approve",
        r"permission to",
        r"proceed\?",
        r"continue\?",
    ]

    text_lower = response_text.lower()
    for pattern in approval_patterns:
        if re.search(pattern, text_lower):
            return True

    return False


def chunk_text(text: str, max_length: int = SLACK_BLOCK_LIMIT) -> list[str]:
    """
    Chunk text into pieces that fit Slack block limits

    Args:
        text: Text to chunk
        max_length: Maximum length per chunk

    Returns:
        List of text chunks
    """
    if len(text) <= max_length:
        return [text]

    chunks = []
    current_chunk = ""

    for line in text.split("\n"):
        if len(current_chunk) + len(line) + 1 > max_length:
            if current_chunk:
                chunks.append(current_chunk)
            current_chunk = line
        else:
            if current_chunk:
                current_chunk += "\n" + line
            else:
                current_chunk = line

    if current_chunk:
        chunks.append(current_chunk)

    return chunks


def format_agent_response(
    agent_name: str,
    response_text: str,
    routing_reason: str,
    response_time: Optional[float] = None,
    session_id: Optional[str] = None,
    show_actions: bool = True,
) -> list[dict[str, Any]]:
    """
    Format agent response as Slack blocks

    Args:
        agent_name: Name of the agent that responded
        response_text: Agent's response text
        routing_reason: Why this agent was selected
        response_time: Response time in seconds
        session_id: Session ID (for display)
        show_actions: Whether to show action buttons

    Returns:
        List of Slack block dictionaries
    """
    blocks = []

    # Header
    blocks.append(
        {
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": f"{EMOJI_ROBOT} *Response from {agent_name}*",
            },
        }
    )

    # Routing context
    context_block: dict[str, Any] = {
        "type": "context",
        "elements": [
            {
                "type": "mrkdwn",
                "text": f"_Agent selected: {routing_reason}_",
            }
        ],
    }
    blocks.append(context_block)

    # Divider
    blocks.append({"type": "divider"})

    # Response content (chunked if needed)
    chunks = chunk_text(response_text)
    for chunk in chunks:
        blocks.append(
            {
                "type": "section",
                "text": {
                    "type": "mrkdwn",
                    "text": chunk,
                },
            }
        )

    # Footer with metadata
    current_time = datetime.now().strftime("%H:%M")
    footer_parts = [f"{EMOJI_CLOCK} _Response at {current_time}"]

    if response_time:
        footer_parts.append(f" • {response_time:.1f}s")

    if session_id:
        # Show last 8 chars of session ID
        footer_parts.append(f" • Session: `{session_id[-8:]}`")

    footer_parts.append("_")

    footer_block: dict[str, Any] = {
        "type": "context",
        "elements": [
            {
                "type": "mrkdwn",
                "text": "".join(footer_parts),
            }
        ],
    }
    blocks.append(footer_block)

    # Action buttons for human-in-the-loop approval
    # Only show if response needs approval
    if show_actions and session_id and needs_approval(response_text):
        # Encode context in button value (session_id|namespace|agent_name)
        button_value = f"{session_id}|{agent_name}"

        # Add approval prompt
        blocks.append({
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": "⚠️ *This action requires your approval*",
            },
        })

        actions_block: dict[str, Any] = {
            "type": "actions",
            "elements": [
                {
                    "type": "button",
                    "text": {"type": "plain_text", "text": "✅ Approve"},
                    "style": "primary",
                    "action_id": "approval_approve",
                    "value": button_value,
                },
                {
                    "type": "button",
                    "text": {"type": "plain_text", "text": "❌ Deny"},
                    "style": "danger",
                    "action_id": "approval_deny",
                    "value": button_value,
                },
            ],
        }
        blocks.append(actions_block)

    return blocks


def format_agent_list(agents: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """
    Format list of agents as Slack blocks

    Args:
        agents: List of agent info dicts

    Returns:
        List of Slack block dictionaries
    """
    blocks = []

    # Header
    blocks.append(
        {
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": f"{EMOJI_ROBOT} *Available Agents*",
            },
        }
    )

    blocks.append({"type": "divider"})

    # Agent list
    for agent in agents:
        status_emoji = ":white_check_mark:" if agent["ready"] else ":x:"

        text = f"*{agent['name']}* (`{agent['namespace']}/{agent['name']}`)\n"
        text += f"{status_emoji} Status: {'Ready' if agent['ready'] else 'Not Ready'}\n"

        if agent.get("description"):
            text += f"_{agent['description']}_"

        blocks.append(
            {
                "type": "section",
                "text": {
                    "type": "mrkdwn",
                    "text": text,
                },
            }
        )

    # Footer
    footer_block: dict[str, Any] = {
        "type": "context",
        "elements": [
            {
                "type": "mrkdwn",
                "text": f"_Total: {len(agents)} agents • Use `/agent-switch <namespace>/<name>` to select one_",
            }
        ],
    }
    blocks.append(footer_block)

    return blocks


def format_error(error_message: str) -> list[dict[str, Any]]:
    """Format error message as Slack blocks"""
    return [
        {
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": f":x: *Error*\n{error_message}",
            },
        },
    ]
