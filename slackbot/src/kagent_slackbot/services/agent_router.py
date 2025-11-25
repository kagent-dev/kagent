"""Agent routing logic"""

import re

from structlog import get_logger

from ..constants import DEFAULT_AGENT_NAME, DEFAULT_AGENT_NAMESPACE
from .agent_discovery import AgentDiscovery

logger = get_logger(__name__)


class AgentRouter:
    """Route user messages to appropriate agents"""

    def __init__(self, agent_discovery: AgentDiscovery):
        self.discovery = agent_discovery
        self.explicit_agent: dict[str, str] = {}  # user_id -> agent_ref

    async def route(self, message: str, user_id: str) -> tuple[str, str, str]:
        """
        Route message to agent

        Args:
            message: User message text
            user_id: User ID (for explicit agent selection)

        Returns:
            Tuple of (namespace, agent_name, reason)
        """

        # Check for explicit agent selection
        if user_id in self.explicit_agent:
            ref = self.explicit_agent[user_id]
            namespace, name = ref.split("/")
            logger.info("Using explicitly selected agent", agent=ref, user=user_id)
            return namespace, name, "explicitly selected by user"

        # Discover available agents
        agents = await self.discovery.discover_agents()

        if not agents:
            logger.warning("No agents available, using default")
            return DEFAULT_AGENT_NAMESPACE, DEFAULT_AGENT_NAME, "default (no agents found)"

        # Score agents based on keyword matching
        scores: dict[str, float] = {}
        message_lower = message.lower()
        message_words = set(re.findall(r"\w+", message_lower))

        for ref, agent in agents.items():
            if not agent.ready:
                continue

            keywords = agent.extract_keywords()
            keyword_set = set(keywords)

            # Calculate match score
            matches = message_words & keyword_set
            if matches:
                # Score based on number of matches and keyword frequency
                score = len(matches)
                scores[ref] = score

        # Select highest scoring agent
        if scores:
            best_agent_ref = max(scores, key=lambda x: scores[x])
            namespace, name = best_agent_ref.split("/")
            score = int(scores[best_agent_ref])
            logger.info(
                "Agent selected via keyword matching",
                agent=best_agent_ref,
                score=score,
                user=user_id,
            )
            return namespace, name, f"matched keywords (score: {score})"

        # No matches, use default
        logger.info("No keyword matches, using default agent", user=user_id)
        return DEFAULT_AGENT_NAMESPACE, DEFAULT_AGENT_NAME, "default (no keyword matches)"

    def set_explicit_agent(self, user_id: str, namespace: str, name: str) -> None:
        """Set explicit agent selection for user"""
        ref = f"{namespace}/{name}"
        self.explicit_agent[user_id] = ref
        logger.info("User selected agent explicitly", user=user_id, agent=ref)

    def clear_explicit_agent(self, user_id: str) -> None:
        """Clear explicit agent selection"""
        if user_id in self.explicit_agent:
            del self.explicit_agent[user_id]
            logger.info("Cleared explicit agent selection", user=user_id)
