"""App mention handlers"""

import time
from typing import Any
from slack_bolt.async_app import AsyncApp
from slack_sdk.web.async_client import AsyncWebClient
from slack_bolt.context.say.async_say import AsyncSay
from structlog import get_logger

from ..services.a2a_client import A2AClient
from ..services.agent_router import AgentRouter
from ..services.agent_discovery import AgentDiscovery
from ..auth.permissions import PermissionChecker
from ..slack.validators import validate_message, sanitize_message, strip_bot_mention
from ..slack.formatters import format_agent_response, format_error
from ..constants import SESSION_ID_PREFIX, EMOJI_THINKING

logger = get_logger(__name__)


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
            use_streaming = agent and agent.type == "Declarative"

            if use_streaming:
                # Streaming response with real-time updates
                working_msg = await say(
                    text=f"{EMOJI_THINKING} Processing your request...",
                    thread_ts=thread_ts,
                )
                working_ts = working_msg["ts"]

                response_text = ""
                last_update = time.time()

                try:
                    async for event in a2a_client.stream_agent(
                        namespace, agent_name, message, session_id, user_id
                    ):
                        # Extract message from event
                        result = event.get("result", {})
                        status = result.get("status", {})

                        if status.get("message"):
                            msg = status["message"]
                            # Only accumulate agent messages, not user messages
                            if msg.get("role") == "agent" or not msg.get("role"):
                                parts = msg.get("parts", [])
                                for part in parts:
                                    if part.get("text"):
                                        response_text += part["text"]

                            # Update every 2 seconds
                            if time.time() - last_update > 2:
                                preview = response_text[:1000] + ("..." if len(response_text) > 1000 else "")
                                await client.chat_update(
                                    channel=channel,
                                    ts=working_ts,
                                    text=preview,
                                )
                                last_update = time.time()

                    # Final update with full formatted response
                    response_time = time.time() - start_time
                    blocks = format_agent_response(
                        agent_name=f"{namespace}/{agent_name}",
                        response_text=response_text or "Agent completed but returned no message.",
                        routing_reason=reason,
                        response_time=response_time,
                        session_id=session_id,
                    )

                    await client.chat_update(
                        channel=channel,
                        ts=working_ts,
                        text=response_text[:100],
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
                # Fallback to synchronous invocation
                result = await a2a_client.invoke_agent(
                    namespace=namespace,
                    agent_name=agent_name,
                    message=message,
                    session_id=session_id,
                    user_id=user_id,
                )

                response_time = time.time() - start_time

                # Extract response text from A2A result
                task = result.get("result", {})
                history = task.get("history", [])

                if history:
                    # Get only agent messages, not user messages
                    agent_messages = [msg for msg in history if msg.get("role") == "agent"]
                    if agent_messages:
                        last_message = agent_messages[-1]
                        parts = last_message.get("parts", [])
                        response_text = "\n".join(part.get("text", "") for part in parts if part.get("text"))
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
