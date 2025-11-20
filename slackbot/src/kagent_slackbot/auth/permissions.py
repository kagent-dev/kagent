"""Agent permission checking"""

from typing import Any

import yaml
from structlog import get_logger

from .slack_groups import SlackGroupChecker

logger = get_logger(__name__)


class PermissionChecker:
    """Check agent access permissions"""

    def __init__(self, config_path: str, group_checker: SlackGroupChecker):
        self.group_checker = group_checker
        self.config = self._load_config(config_path)
        self.permissions = self.config.get("agent_permissions", {})

    def _load_config(self, path: str) -> dict[str, Any]:
        """Load permissions from YAML"""
        try:
            with open(path) as f:
                config = yaml.safe_load(f)
                return config if config else {}
        except FileNotFoundError:
            logger.warning("Permissions config not found", path=path)
            return {}
        except Exception as e:
            logger.error("Failed to load permissions config", path=path, error=str(e))
            return {}

    async def can_access_agent(
        self,
        user_id: str,
        agent_ref: str,
    ) -> tuple[bool, str]:
        """
        Check if user can access agent

        Args:
            user_id: Slack user ID
            agent_ref: Agent reference (namespace/name)

        Returns:
            Tuple of (allowed, reason)
        """

        # If agent not in config, allow by default (public agent)
        if agent_ref not in self.permissions:
            return True, "public agent"

        perms = self.permissions[agent_ref]

        # Get user email
        user_email = await self.group_checker.get_user_email(user_id)

        # Check specific users allowlist
        if user_email in perms.get("users", []):
            logger.info("User allowed via allowlist", user=user_id, agent=agent_ref)
            return True, "user allowlist"

        # Check user groups
        for group_id in perms.get("user_groups", []):
            if await self.group_checker.is_user_in_group(user_id, group_id):
                logger.info(
                    "User allowed via group",
                    user=user_id,
                    agent=agent_ref,
                    group=group_id,
                )
                return True, "group membership"

        # If both lists empty, agent is public
        if not perms.get("users") and not perms.get("user_groups"):
            return True, "public agent"

        # Denied
        deny_msg = perms.get("deny_message", f"Access denied to {agent_ref}")

        logger.warning(
            "User denied access to agent",
            user=user_id,
            agent=agent_ref,
        )

        return False, deny_msg

    async def filter_agents_by_user(
        self,
        user_id: str,
        agents: list[dict[str, Any]],
    ) -> list[dict[str, Any]]:
        """
        Filter agent list to only show accessible agents

        Args:
            user_id: Slack user ID
            agents: List of agent info dicts

        Returns:
            Filtered list of agents user can access
        """
        allowed = []

        for agent in agents:
            ref = f"{agent['namespace']}/{agent['name']}"
            can_access, _ = await self.can_access_agent(user_id, ref)

            if can_access:
                allowed.append(agent)
            else:
                logger.debug("Agent filtered out for user", user=user_id, agent=ref)

        logger.info(
            "Filtered agents for user",
            user=user_id,
            total=len(agents),
            allowed=len(allowed),
        )

        return allowed
