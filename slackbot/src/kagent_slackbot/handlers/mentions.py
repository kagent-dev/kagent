"""App mention handlers"""

import time
from typing import Any

from a2a.types import (
    DataPart,
    Message,
    Part,
    Task,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from slack_bolt.async_app import AsyncApp
from slack_bolt.context.say.async_say import AsyncSay
from slack_sdk.web.async_client import AsyncWebClient
from structlog import get_logger

from ..auth.permissions import PermissionChecker
from ..constants import EMOJI_THINKING, SESSION_ID_PREFIX
from ..models.interrupt import ActionRequest, InterruptData, ReviewConfig
from ..services.a2a_client import A2AClient
from ..services.agent_discovery import AgentDiscovery
from ..services.agent_router import AgentRouter
from ..slack.formatters import format_agent_response, format_error
from ..slack.validators import sanitize_message, strip_bot_mention, validate_message

logger = get_logger(__name__)


async def handle_interrupt_approval(
    client: AsyncWebClient,
    channel: str,
    message_ts: str,
    interrupt_status: TaskStatus,  # Now typed!
    session_id: str,
    task_id: str,
    agent_full_name: str,
    response_text: str,
) -> None:
    """Handle HITL interrupt approval request.

    Args:
        client: Slack client
        channel: Channel ID
        message_ts: Message timestamp to update
        interrupt_status: TaskStatus with message containing interrupt data
        session_id: Session ID for resume
        task_id: Task ID of the interrupted task
        agent_full_name: Full agent name (namespace/name)
        response_text: Accumulated response text so far
    """
    # Extract interrupt data from status message
    if not interrupt_status.message:
        logger.warning("Interrupt status has no message")
        return

    action_requests: list[ActionRequest] = []
    review_configs: list[ReviewConfig] = []

    # Find the DataPart with interrupt information
    for part in interrupt_status.message.parts:
        if isinstance(part.root, DataPart):
            try:
                # Validate and parse interrupt data
                interrupt_data = InterruptData.model_validate(part.root.data)
                action_requests = interrupt_data.action_requests
                review_configs = interrupt_data.review_configs
                break
            except Exception as e:
                logger.warning("Failed to parse interrupt data", error=str(e))
                continue

    if not action_requests:
        logger.warning("No action requests found in interrupt")
        return

    # Generate approval UI
    from ..slack.formatters import format_approval_request

    blocks = format_approval_request(
        agent_name=agent_full_name,
        response_text=response_text,
        action_requests=action_requests,
        review_configs=review_configs,
        session_id=session_id,
        task_id=task_id,
    )

    # Update message with approval UI
    # Extract text summary for the text field (Slack requires non-empty text)
    if response_text and response_text.strip():
        text_summary = response_text[:200]
    else:
        text_summary = "⚠️ Approval Required - Agent needs your decision"

    await client.chat_update(
        channel=channel,
        ts=message_ts,
        text=text_summary,
        blocks=blocks,
    )

    logger.info(
        "Showing approval UI",
        session=session_id,
        agent=agent_full_name,
        num_actions=len(action_requests),
    )


def register_mention_handlers(
    app: AsyncApp,
    a2a_client: A2AClient,
    agent_router: AgentRouter,
    agent_discovery: AgentDiscovery,
    permission_checker: PermissionChecker,
) -> None:
    """Register app mention and DM handlers"""

    async def process_user_message(
        event: dict[str, Any],
        say: AsyncSay,
        client: AsyncWebClient,
        is_dm: bool = False,
    ) -> None:
        """Shared logic for processing messages from @mentions or DMs"""

        user_id = event["user"]
        channel = event["channel"]
        text = event["text"]
        thread_ts = event.get("thread_ts", event["ts"])

        logger.info(
            "Received message",
            user=user_id,
            channel=channel,
            thread_ts=thread_ts,
            is_dm=is_dm,
        )

        # Acknowledge with reaction
        try:
            await client.reactions_add(
                channel=channel,
                timestamp=event["ts"],
                name="eyes",
            )
        except Exception as e:
            logger.warning("Failed to add reaction", error=str(e))

        # Strip bot mention (for @mentions) and validate
        if not is_dm:
            message = strip_bot_mention(text)
        else:
            message = text
        message = sanitize_message(message)

        if not validate_message(message):
            await say(
                blocks=format_error("Please provide a message after mentioning me!"),
                thread_ts=thread_ts,
            )
            return

        # Build session ID (includes thread_ts to isolate thread contexts)
        session_id = f"{SESSION_ID_PREFIX}-{user_id}-{channel}-{thread_ts}"

        try:
            # Route to agent
            start_time = time.time()
            namespace, agent_name, reason = await agent_router.route(message, user_id)

            # Check permissions (RBAC)
            agent_ref = f"{namespace}/{agent_name}"
            can_access, access_reason = await permission_checker.can_access_agent(user_id, agent_ref)

            if not can_access:
                await say(
                    blocks=format_error(f"⛔ {access_reason}"),
                    thread_ts=thread_ts,
                )
                logger.warning(
                    "User denied access to agent",
                    user=user_id,
                    agent=agent_ref,
                    reason=access_reason,
                )
                return

            # Check if agent supports streaming
            agent = await agent_discovery.get_agent(namespace, agent_name)
            # Enable streaming for both Declarative and BYO agents
            use_streaming = agent and agent.type in ["Declarative", "BYO"]

            if use_streaming:
                # Streaming response with real-time updates
                working_msg = await say(
                    text=f"{EMOJI_THINKING} Processing your request...",
                    thread_ts=thread_ts,
                )
                working_ts = working_msg["ts"]

                response_text = ""
                last_update = time.time()
                pending_interrupt = None
                pending_interrupt_task_id = None
                pending_interrupt_context_id = None

                try:
                    async for event in a2a_client.stream_agent(namespace, agent_name, message, session_id, user_id):
                        # Handle different event types
                        if isinstance(event, TaskStatusUpdateEvent):
                            task_state = event.status.state

                            # Detect input_required state (interrupt)
                            if task_state == TaskState.input_required:
                                pending_interrupt = event.status
                                pending_interrupt_task_id = event.task_id
                                pending_interrupt_context_id = event.context_id
                                break  # Stop streaming, show approval UI

                            # Extract message text from agent
                            if event.status.message:
                                msg = event.status.message
                                # Only accumulate agent messages
                                if msg.role == "agent":
                                    for part in msg.parts:
                                        if isinstance(part.root, TextPart):
                                            response_text += part.root.text

                        elif isinstance(event, TaskArtifactUpdateEvent):
                            # Artifact updates REPLACE content (not append)
                            # Each artifact is a complete message unit
                            artifact_text = ""
                            for part in event.artifact.parts:
                                if isinstance(part.root, TextPart):
                                    artifact_text += part.root.text

                            # Replace response_text with the latest artifact
                            response_text = artifact_text

                            # Update every 2 seconds (only if we have content)
                            if time.time() - last_update > 2 and response_text.strip():
                                preview = response_text[:1000] + ("..." if len(response_text) > 1000 else "")
                                await client.chat_update(
                                    channel=channel,
                                    ts=working_ts,
                                    text=preview,
                                )
                                last_update = time.time()

                    # Handle interrupt if detected
                    if pending_interrupt:
                        await handle_interrupt_approval(
                            client=client,
                            channel=channel,
                            message_ts=working_ts,
                            interrupt_status=pending_interrupt,
                            session_id=pending_interrupt_context_id
                            or session_id,  # Use agent's context ID if available!
                            task_id=pending_interrupt_task_id,
                            agent_full_name=f"{namespace}/{agent_name}",
                            response_text=response_text,
                        )
                        return  # Don't send final response yet

                    # Final update with full formatted response
                    response_time = time.time() - start_time
                    blocks = format_agent_response(
                        agent_name=f"{namespace}/{agent_name}",
                        response_text=response_text or "Agent completed but returned no message.",
                        routing_reason=reason,
                        response_time=response_time,
                        session_id=session_id,
                    )

                    # Ensure text is non-empty (Slack API requirement)
                    text_field = response_text[:200] if response_text and response_text.strip() else "Agent response"

                    await client.chat_update(
                        channel=channel,
                        ts=working_ts,
                        text=text_field,
                        blocks=blocks,
                    )

                    logger.info(
                        "Successfully processed streaming message",
                        user=user_id,
                        agent=f"{namespace}/{agent_name}",
                        response_time=response_time,
                    )

                except Exception as e:
                    # If streaming fails, update message with error and exit cleanly
                    logger.error("Streaming failed", error=str(e), exc_info=True)
                    await client.chat_update(
                        channel=channel,
                        ts=working_ts,
                        blocks=format_error(f"Sorry, streaming failed: {str(e)}"),
                    )
                    return  # Exit cleanly - error already shown to user

            else:
                # Non-streaming mode
                result = await a2a_client.invoke_agent(
                    namespace=namespace,
                    agent_name=agent_name,
                    message=message,
                    session_id=session_id,
                    task_id=None,
                    user_id=user_id,
                )
                # result is now Task!

                response_time = time.time() - start_time

                response_text = ""
                if result.history:
                    # Filter to agent messages
                    agent_messages = [msg for msg in result.history if msg.role == "agent"]
                    if agent_messages:
                        last_message = agent_messages[-1]
                        # Extract text from parts
                        text_parts = [part.root.text for part in last_message.parts if isinstance(part.root, TextPart)]
                        response_text = "\n".join(text_parts)
                    else:
                        response_text = "Agent responded but no message was returned."
                else:
                    response_text = "Agent responded but no message was returned."

                # Format and send response
                blocks = format_agent_response(
                    agent_name=f"{namespace}/{agent_name}",
                    response_text=response_text,
                    routing_reason=reason,
                    response_time=response_time,
                    session_id=session_id,
                )

                await say(blocks=blocks, thread_ts=thread_ts)

                logger.info(
                    "Successfully processed mention",
                    user=user_id,
                    agent=f"{namespace}/{agent_name}",
                    response_time=response_time,
                )

        except Exception as e:
            logger.error(
                "Failed to process mention",
                user=user_id,
                error=str(e),
                exc_info=True,
            )

            await say(
                blocks=format_error(
                    f"Sorry, I encountered an error: {str(e)}\n\n"
                    "Please try again or contact support if the issue persists."
                ),
                thread_ts=thread_ts,
            )

    # Register event handlers
    @app.event("app_mention")
    async def handle_mention(
        event: dict[str, Any],
        say: AsyncSay,
        client: AsyncWebClient,
    ) -> None:
        """Handle @bot mentions in channels"""
        await process_user_message(event, say, client, is_dm=False)

    @app.event("message")
    async def handle_dm(
        event: dict[str, Any],
        say: AsyncSay,
        client: AsyncWebClient,
    ) -> None:
        """Handle direct messages to bot"""
        # Only handle DMs - Bolt's ignoring_self_events middleware filters bot messages automatically
        if event.get("channel_type") == "im":
            await process_user_message(event, say, client, is_dm=True)
