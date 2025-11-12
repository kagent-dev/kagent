"""Slash command handlers"""

from typing import Any

from slack_bolt.async_app import AsyncApp
from slack_bolt.context.ack.async_ack import AsyncAck
from slack_bolt.context.respond.async_respond import AsyncRespond
from structlog import get_logger

from ..auth.permissions import PermissionChecker
from ..constants import EMOJI_ROBOT
from ..services.agent_discovery import AgentDiscovery
from ..services.agent_router import AgentRouter
from ..slack.formatters import format_agent_list, format_error

logger = get_logger(__name__)


def register_command_handlers(
    app: AsyncApp,
    agent_discovery: AgentDiscovery,
    agent_router: AgentRouter,
    permission_checker: PermissionChecker,
) -> None:
    """Register slash command handlers"""

    @app.command("/agents")
    async def handle_agents_command(
        ack: AsyncAck,
        command: dict[str, Any],
        respond: AsyncRespond,
    ) -> None:
        """List available agents"""
        await ack()

        user_id = command["user_id"]

        logger.info("Listing agents", user=user_id)

        try:
            # Discover agents
            agents_dict = await agent_discovery.discover_agents()

            # Format for display
            agents_list = [
                {
                    "namespace": agent.namespace,
                    "name": agent.name,
                    "description": agent.description,
                    "ready": agent.ready,
                }
                for agent in agents_dict.values()
            ]

            # Sort by namespace/name
            agents_list.sort(key=lambda a: (a["namespace"], a["name"]))

            # Filter by user permissions (RBAC)
            agents_list = await permission_checker.filter_agents_by_user(user_id, agents_list)

            if not agents_list:
                await respond(
                    blocks=format_error("No agents available or accessible to you at the moment."),
                    response_type="ephemeral",
                )
                return

            blocks = format_agent_list(agents_list)
            await respond(blocks=blocks, response_type="ephemeral")

            logger.info("Listed agents", user=user_id, count=len(agents_list))

        except Exception as e:
            logger.error("Failed to list agents", user=user_id, error=str(e))
            await respond(
                blocks=format_error(f"Failed to fetch agents: {str(e)}"),
                response_type="ephemeral",
            )

    @app.command("/agent-switch")
    async def handle_agent_switch_command(
        ack: AsyncAck,
        command: dict[str, Any],
        respond: AsyncRespond,
    ) -> None:
        """Switch to specific agent"""
        await ack()

        user_id = command["user_id"]
        text = command.get("text", "").strip()

        logger.info("Agent switch requested", user=user_id, text=text)

        # Handle reset command
        if text.lower() == "reset":
            agent_router.clear_explicit_agent(user_id)

            await respond(
                text=(
                    ":recycle: *Agent selection reset*\n\n"
                    "I'll now automatically select the best agent based on your message."
                ),
                response_type="ephemeral",
            )

            logger.info("Agent selection reset", user=user_id)
            return

        if not text:
            await respond(
                text=(
                    f"{EMOJI_ROBOT} *Agent Switch*\n\n"
                    "Usage: `/agent-switch <namespace>/<name>`\n\n"
                    "Example: `/agent-switch kagent/k8s-agent`\n\n"
                    "Use `/agents` to see available agents."
                ),
                response_type="ephemeral",
            )
            return

        # Parse namespace/name
        if "/" not in text:
            await respond(
                blocks=format_error(
                    "Invalid format. Use: `/agent-switch <namespace>/<name>`\nExample: `/agent-switch kagent/k8s-agent`"
                ),
                response_type="ephemeral",
            )
            return

        try:
            namespace, name = text.split("/", 1)
            namespace = namespace.strip()
            name = name.strip()

            # Verify agent exists
            agent = await agent_discovery.get_agent(namespace, name)

            if not agent:
                await respond(
                    blocks=format_error(
                        f"Agent `{namespace}/{name}` not found.\nUse `/agents` to see available agents."
                    ),
                    response_type="ephemeral",
                )
                return

            if not agent.ready:
                await respond(
                    blocks=format_error(
                        f"Agent `{namespace}/{name}` exists but is not ready.\n"
                        "Please try again later or choose a different agent."
                    ),
                    response_type="ephemeral",
                )
                return

            # Check permissions (RBAC)
            agent_ref = f"{namespace}/{name}"
            can_access, access_reason = await permission_checker.can_access_agent(user_id, agent_ref)

            if not can_access:
                await respond(
                    blocks=format_error(f"⛔ {access_reason}"),
                    response_type="ephemeral",
                )
                logger.warning("User denied access to agent", user=user_id, agent=agent_ref)
                return

            # Set explicit agent selection
            agent_router.set_explicit_agent(user_id, namespace, name)

            await respond(
                text=(
                    f":white_check_mark: *Switched to {namespace}/{name}*\n\n"
                    f"_{agent.description}_\n\n"
                    "Your next messages will be routed to this agent.\n"
                    "To return to automatic routing, use `/agent-switch reset`"
                ),
                response_type="ephemeral",
            )

            logger.info(
                "Agent switched",
                user=user_id,
                agent=f"{namespace}/{name}",
            )

        except ValueError:
            await respond(
                blocks=format_error("Invalid format. Use: `/agent-switch <namespace>/<name>`"),
                response_type="ephemeral",
            )
        except Exception as e:
            logger.error("Failed to switch agent", user=user_id, error=str(e))
            await respond(
                blocks=format_error(f"Failed to switch agent: {str(e)}"),
                response_type="ephemeral",
            )
