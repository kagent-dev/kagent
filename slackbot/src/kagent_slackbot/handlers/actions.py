"""Action (button) handlers"""

from typing import Any

from a2a.types import (
    DataPart,
    Part,
    TaskArtifactUpdateEvent,
    TaskStatusUpdateEvent,
    TextPart,
)
from slack_bolt.async_app import AsyncApp
from slack_sdk.web.async_client import AsyncWebClient
from structlog import get_logger

from ..services.a2a_client import A2AClient

logger = get_logger(__name__)


def _parse_button_value(action: dict[str, Any]) -> tuple[str, str | None, str]:
    """
    Parse approval button value into components.

    Args:
        action: Slack action dict containing button value

    Returns:
        Tuple of (session_id, task_id, agent_full_name)
    """
    button_value = action["value"]
    parts = button_value.split("|")
    session_id = parts[0]
    task_id = parts[1] if len(parts) > 1 else None
    agent_full_name = parts[2] if len(parts) > 2 else ""
    return session_id, task_id, agent_full_name


def _extract_original_message_ts(session_id: str) -> str | None:
    """
    Extract original message timestamp from session ID.

    Session ID format: slack-{user_id}-{channel}-{thread_ts}
    For top-level messages, thread_ts == original message ts

    Args:
        session_id: Session ID string

    Returns:
        Original message timestamp or None if parsing fails
    """
    parts = session_id.split("-")
    if len(parts) >= 4:
        return parts[3]  # thread_ts
    return None


async def _remove_reaction(
    client: AsyncWebClient,
    channel: str,
    timestamp: str,
) -> None:
    """
    Remove eyes reaction from a message.

    Args:
        client: Slack client
        channel: Channel ID
        timestamp: Message timestamp
    """
    try:
        await client.reactions_remove(
            channel=channel,
            timestamp=timestamp,
            name="eyes",
        )
    except Exception as e:
        logger.warning("Failed to remove reaction", error=str(e))


def register_action_handlers(app: AsyncApp, a2a_client: A2AClient) -> None:
    """Register action handlers for interactive buttons"""

    @app.action("approval_approve")
    async def handle_approval_approve(
        ack: Any,
        action: dict[str, Any],
        body: dict[str, Any],
        client: AsyncWebClient,
    ) -> None:
        """Handle approval button click"""
        await ack()

        session_id, task_id, agent_full_name = _parse_button_value(action)

        user_id = body["user"]["id"]
        channel = body["container"]["channel_id"]
        message_ts = body["container"]["message_ts"]

        logger.info(
            "User approved action",
            user=user_id,
            session=session_id,
            task_id=task_id,
            agent=agent_full_name,
        )

        # Send approval message back to agent in same session
        if "/" in agent_full_name:
            namespace, agent_name = agent_full_name.split("/", 1)

            try:
                # Update UI to show approval was received
                await client.chat_update(
                    channel=channel,
                    ts=message_ts,
                    text="✅ Approved - Agent is processing...",
                    blocks=body["message"]["blocks"]
                    + [
                        {
                            "type": "context",
                            "elements": [
                                {
                                    "type": "mrkdwn",
                                    "text": f"✅ _Approved by <@{user_id}> - agent working..._",
                                }
                            ],
                        }
                    ],
                )

                # Build structured approval response using SDK types
                approval_parts: list[Part] = [
                    TextPart(text="APPROVED: User approved. Proceed with the action."),
                    DataPart(data={"decision_type": "tool_approval", "decision": "approve"}),
                ]

                response_text = ""

                async for event in a2a_client.stream_agent_with_parts(
                    namespace=namespace,
                    agent_name=agent_name,
                    parts=approval_parts,
                    session_id=session_id,
                    user_id=user_id,
                    task_id=task_id,
                ):
                    # Handle different event types
                    if isinstance(event, TaskStatusUpdateEvent):
                        # Collect agent response messages
                        if event.status.message:
                            msg = event.status.message
                            if msg.role == "agent":
                                for part in msg.parts:
                                    if isinstance(part.root, TextPart):
                                        response_text += part.root.text

                    elif isinstance(event, TaskArtifactUpdateEvent):
                        # Artifact updates REPLACE content (not append)
                        artifact_text = ""
                        for part in event.artifact.parts:
                            if isinstance(part.root, TextPart):
                                artifact_text += part.root.text
                        response_text = artifact_text

                # Update with final result
                await client.chat_update(
                    channel=channel,
                    ts=message_ts,
                    text=f"✅ Completed: {response_text[:200] if response_text else 'Action completed successfully'}",
                    blocks=body["message"]["blocks"]
                    + [
                        {
                            "type": "context",
                            "elements": [
                                {
                                    "type": "mrkdwn",
                                    "text": f"✅ _Approved by <@{user_id}> - completed_",
                                }
                            ],
                        },
                        {
                            "type": "section",
                            "text": {
                                "type": "mrkdwn",
                                "text": response_text if response_text else "_Action completed_",
                            },
                        },
                    ],
                )

                # Remove acknowledgment reaction from original message
                original_msg_ts = _extract_original_message_ts(session_id)
                if original_msg_ts:
                    await _remove_reaction(client, channel, original_msg_ts)

                logger.info("Approval completed", session=session_id, agent=agent_full_name)

            except Exception as e:
                logger.error("Failed to send approval", error=str(e), session=session_id)
                await client.chat_postEphemeral(
                    channel=channel,
                    user=user_id,
                    text=f"❌ Failed to send approval to agent: {str(e)}",
                )

                # Remove acknowledgment reaction even on error
                original_msg_ts = _extract_original_message_ts(session_id)
                if original_msg_ts:
                    await _remove_reaction(client, channel, original_msg_ts)

    @app.action("approval_deny")
    async def handle_approval_deny(
        ack: Any,
        action: dict[str, Any],
        body: dict[str, Any],
        client: AsyncWebClient,
    ) -> None:
        """Handle denial button click"""
        await ack()

        session_id, task_id, agent_full_name = _parse_button_value(action)

        user_id = body["user"]["id"]
        channel = body["container"]["channel_id"]
        message_ts = body["container"]["message_ts"]

        logger.info(
            "User denied action",
            user=user_id,
            session=session_id,
            task_id=task_id,
            agent=agent_full_name,
        )

        # Send denial message back to agent
        if "/" in agent_full_name:
            namespace, agent_name = agent_full_name.split("/", 1)

            try:
                await a2a_client.invoke_agent(
                    namespace=namespace,
                    agent_name=agent_name,
                    message="DENIED: User denied. Cancel the action and do not proceed.",
                    session_id=session_id,
                    task_id=task_id,  # Include task_id to resume existing task!
                    user_id=user_id,
                )

                await client.chat_update(
                    channel=channel,
                    ts=message_ts,
                    text="❌ Denied - Agent will not proceed",
                    blocks=body["message"]["blocks"]
                    + [
                        {
                            "type": "context",
                            "elements": [
                                {
                                    "type": "mrkdwn",
                                    "text": f"❌ _Denied by <@{user_id}> - agent canceled_",
                                }
                            ],
                        }
                    ],
                )

                # Remove acknowledgment reaction from original message
                original_msg_ts = _extract_original_message_ts(session_id)
                if original_msg_ts:
                    await _remove_reaction(client, channel, original_msg_ts)

                logger.info("Denial sent to agent", session=session_id, agent=agent_full_name)

            except Exception as e:
                logger.error("Failed to send denial", error=str(e), session=session_id)
                await client.chat_postEphemeral(
                    channel=channel,
                    user=user_id,
                    text=f"❌ Failed to send denial to agent: {str(e)}",
                )

                # Remove acknowledgment reaction even on error
                original_msg_ts = _extract_original_message_ts(session_id)
                if original_msg_ts:
                    await _remove_reaction(client, channel, original_msg_ts)
