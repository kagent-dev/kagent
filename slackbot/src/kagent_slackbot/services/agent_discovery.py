"""Agent discovery from Kagent API"""

import time
import httpx
from typing import Optional, Any
from structlog import get_logger
from ..constants import AGENT_CACHE_TTL

logger = get_logger(__name__)


class AgentInfo:
    """Information about an agent"""

    def __init__(self, data: dict[str, Any]):
        self.namespace = data["agent"]["metadata"]["namespace"]
        self.name = data["agent"]["metadata"]["name"]
        self.description = data["agent"]["spec"].get("description", "")
        self.type = data["agent"]["spec"]["type"]
        self.ready = self._check_ready(data["agent"]["status"])

        # Extract skills from a2aConfig if available
        self.skills = []
        a2a_config = data["agent"]["spec"].get("declarative", {}).get("a2aConfig", {})
        if a2a_config:
            self.skills = a2a_config.get("skills", [])

    def _check_ready(self, status: dict[str, Any]) -> bool:
        """Check if agent is ready"""
        conditions = status.get("conditions", [])
        for condition in conditions:
            if condition.get("type") == "Ready" and condition.get("status") == "True":
                return True
        return False

    @property
    def ref(self) -> str:
        """Agent reference string"""
        return f"{self.namespace}/{self.name}"

    def extract_keywords(self) -> list[str]:
        """Extract routing keywords from agent metadata"""
        keywords = []

        # From description
        if self.description:
            # Simple word extraction (can be made more sophisticated)
            words = self.description.lower().split()
            keywords.extend(words)

        # From skills
        for skill in self.skills:
            skill_desc = skill.get("description", "").lower()
            keywords.extend(skill_desc.split())
            keywords.extend(skill.get("tags", []))

        return list(set(keywords))  # Deduplicate


class AgentDiscovery:
    """Discover agents from Kagent API"""

    def __init__(self, base_url: str, timeout: int = 30):
        self.base_url = base_url.rstrip("/")
        self.client = httpx.AsyncClient(timeout=timeout)
        self.cache: dict[str, AgentInfo] = {}
        self.last_refresh = 0.0

    async def discover_agents(self, force_refresh: bool = False) -> dict[str, AgentInfo]:
        """
        Discover available agents

        Args:
            force_refresh: Force cache refresh

        Returns:
            Dict mapping agent ref to AgentInfo
        """
        now = time.time()

        if not force_refresh and (now - self.last_refresh) < AGENT_CACHE_TTL:
            logger.debug("Using cached agent list", count=len(self.cache))
            return self.cache

        logger.info("Discovering agents from Kagent API")

        try:
            url = f"{self.base_url}/api/agents"
            response = await self.client.get(url)
            response.raise_for_status()

            data = response.json()
            agents_data = data.get("data", [])

            # Build cache
            self.cache = {}
            for agent_data in agents_data:
                agent_info = AgentInfo(agent_data)
                self.cache[agent_info.ref] = agent_info

            self.last_refresh = now

            logger.info("Agent discovery complete", count=len(self.cache))
            return self.cache

        except Exception as e:
            logger.error("Agent discovery failed", error=str(e))
            # Return cached agents if available
            if self.cache:
                logger.warning("Using stale agent cache")
                return self.cache
            raise

    async def get_agent(self, namespace: str, name: str) -> Optional[AgentInfo]:
        """Get specific agent info"""
        agents = await self.discover_agents()
        return agents.get(f"{namespace}/{name}")

    async def close(self) -> None:
        """Close HTTP client"""
        await self.client.aclose()
