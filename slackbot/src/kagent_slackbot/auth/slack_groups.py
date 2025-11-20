"""Slack user group membership checking"""

import time

from slack_sdk.web.async_client import AsyncWebClient
from structlog import get_logger

logger = get_logger(__name__)


class SlackGroupChecker:
    """Check Slack user group membership with caching"""

    def __init__(self, client: AsyncWebClient, cache_ttl: int = 300):
        self.client = client
        self.cache_ttl = cache_ttl
        self.cache: dict[str, tuple[set[str], float]] = {}
        self.email_cache: dict[str, tuple[str, float]] = {}

    async def is_user_in_group(self, user_id: str, group_id: str) -> bool:
        """
        Check if user is in Slack user group

        Args:
            user_id: Slack user ID
            group_id: Slack user group ID

        Returns:
            True if user is in group
        """
        # Check cache
        if group_id in self.cache:
            members, timestamp = self.cache[group_id]
            if time.time() - timestamp < self.cache_ttl:
                return user_id in members

        # Fetch from Slack API
        try:
            response = await self.client.usergroups_users_list(usergroup=group_id)
            members = set(response["users"])

            # Update cache
            self.cache[group_id] = (members, time.time())

            result = user_id in members

            logger.debug(
                "Checked group membership",
                user=user_id,
                group=group_id,
                is_member=result,
            )

            return result

        except Exception as e:
            logger.error(
                "Failed to check group membership",
                user=user_id,
                group=group_id,
                error=str(e),
            )
            return False

    async def get_user_email(self, user_id: str) -> str:
        """
        Get user email from Slack

        Args:
            user_id: Slack user ID

        Returns:
            User email address (lowercase)
        """
        # Check cache
        if user_id in self.email_cache:
            email, timestamp = self.email_cache[user_id]
            if time.time() - timestamp < self.cache_ttl:
                return email

        # Fetch from Slack API
        try:
            response = await self.client.users_info(user=user_id)
            user = response["user"]
            email = str(user["profile"]["email"]).lower()

            # Update cache
            self.email_cache[user_id] = (email, time.time())

            logger.debug("Retrieved user email", user=user_id, email=email)

            return email

        except Exception as e:
            logger.error("Failed to get user email", user=user_id, error=str(e))
            return ""
