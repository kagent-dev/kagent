"""Slack Block Kit formatting"""

from datetime import datetime
from typing import Any, Optional

from ..constants import EMOJI_CLOCK, EMOJI_ROBOT, SLACK_BLOCK_LIMIT
from ..models.interrupt import ActionRequest, ReviewConfig


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


def format_approval_request(
    agent_name: str,
    response_text: str,
    action_requests: list[ActionRequest],  # Now typed!
    review_configs: list[ReviewConfig],  # Now typed!
    session_id: str,
    task_id: str,
) -> list[dict[str, Any]]:
    """Format tool approval request as Slack blocks.

    Args:
        agent_name: Name of the agent requesting approval
        response_text: Agent's explanation text
        action_requests: List of typed ActionRequest objects
        review_configs: List of typed ReviewConfig objects
        session_id: Session ID for button callbacks
        task_id: Task ID of the interrupted task

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
                "text": f"{EMOJI_ROBOT} *Approval Required from {agent_name}*",
            },
        }
    )

    # Agent's explanation (if any)
    if response_text:
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

    blocks.append({"type": "divider"})

    # List each action requiring approval
    blocks.append(
        {
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": "⚠️ *The following actions require your approval:*",
            },
        }
    )

    for action in action_requests:
        # Now use properties instead of .get()!
        tool_name = action.name
        tool_args = action.args

        # Format args nicely
        if tool_args:
            args_text = "\n".join([f"  • `{k}`: `{v}`" for k, v in tool_args.items()])
            tool_text = f"**Tool**: `{tool_name}`\n**Arguments**:\n{args_text}"
        else:
            tool_text = f"**Tool**: `{tool_name}`\n_(no arguments)_"

        blocks.append(
            {
                "type": "section",
                "text": {
                    "type": "mrkdwn",
                    "text": tool_text,
                },
            }
        )

    # Approval buttons - include task_id!
    button_value = f"{session_id}|{task_id}|{agent_name}"

    blocks.append(
        {
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
    )

    # Footer
    blocks.append(
        {
            "type": "context",
            "elements": [
                {
                    "type": "mrkdwn",
                    "text": f"_Session: `{session_id[-8:]}` • Waiting for your decision_",
                }
            ],
        }
    )

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
