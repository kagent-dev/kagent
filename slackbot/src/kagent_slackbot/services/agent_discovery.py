"""Agent discovery from Kagent API"""

import time
from typing import Any, Optional

import httpx
from pydantic import BaseModel, Field, computed_field, field_validator
from structlog import get_logger

from ..constants import AGENT_CACHE_TTL

logger = get_logger(__name__)


class AgentCondition(BaseModel):
    """Kubernetes-style status condition"""

    type: str
    status: str
    reason: Optional[str] = None
    message: Optional[str] = None


class AgentSkill(BaseModel):
    """Agent skill definition (from A2A protocol)"""

    id: str
    name: str
    description: str = ""
    tags: list[str] = Field(default_factory=list)
    examples: list[str] = Field(default_factory=list)
    inputModes: list[str] = Field(default_factory=list, alias="inputModes")
    outputModes: list[str] = Field(default_factory=list, alias="outputModes")


class AgentMetadata(BaseModel):
    """Agent metadata"""

    namespace: str
    name: str


class AgentA2AConfig(BaseModel):
    """Agent A2A configuration"""

    skills: list[AgentSkill] = Field(default_factory=list)


class AgentDeclarative(BaseModel):
    """Agent declarative configuration"""

    a2aConfig: Optional[AgentA2AConfig] = Field(default=None, alias="a2aConfig")


class AgentSpec(BaseModel):
    """Agent specification"""

    type: str
    description: str = ""
    declarative: Optional[AgentDeclarative] = None


class AgentStatus(BaseModel):
    """Agent status"""

    conditions: list[AgentCondition] = Field(default_factory=list)


class Agent(BaseModel):
    """Agent resource"""

    metadata: AgentMetadata
    spec: AgentSpec
    status: AgentStatus


class AgentResponse(BaseModel):
    """
    Agent response from Kagent API /api/agents endpoint.
    This matches the AgentResponse struct from the Go backend.
    """

    id: str
    agent: Agent
    model: str = ""
    modelProvider: str = ""
    modelConfigRef: str = ""
    tools: Optional[list[dict[str, Any]]] = None
    deploymentReady: bool = False
    accepted: bool = False

    @field_validator("tools", mode="before")
    @classmethod
    def convert_none_to_empty_list(cls, v):
        """Convert None to empty list for tools field"""
        return v if v is not None else []


class AgentInfo(BaseModel):
    """
    Wrapper around AgentResponse with convenient computed properties.
    Used for agent routing and display in Slack.
    """

    # Store the full API response
    id: str
    agent: Agent
    model: str = ""
    modelProvider: str = ""
    modelConfigRef: str = ""
    tools: Optional[list[dict[str, Any]]] = None
    deploymentReady: bool = False
    accepted: bool = False

    @field_validator("tools", mode="before")
    @classmethod
    def convert_none_to_empty_list(cls, v):
        """Convert None to empty list for tools field"""
        return v if v is not None else []

    @computed_field  # type: ignore[misc]
    @property
    def namespace(self) -> str:
        """Agent namespace"""
        return self.agent.metadata.namespace

    @computed_field  # type: ignore[misc]
    @property
    def name(self) -> str:
        """Agent name"""
        return self.agent.metadata.name

    @computed_field  # type: ignore[misc]
    @property
    def description(self) -> str:
        """Agent description"""
        return self.agent.spec.description

    @computed_field  # type: ignore[misc]
    @property
    def type(self) -> str:
        """Agent type"""
        return self.agent.spec.type

    @computed_field  # type: ignore[misc]
    @property
    def ready(self) -> bool:
        """
        Check if agent is ready.
        Uses the pre-computed deploymentReady field from the backend.
        """
        return self.deploymentReady

    @computed_field  # type: ignore[misc]
    @property
    def skills(self) -> list[AgentSkill]:
        """Extract skills from agent configuration"""
        if self.agent.spec.declarative and self.agent.spec.declarative.a2aConfig:
            return self.agent.spec.declarative.a2aConfig.skills
        return []

    @computed_field  # type: ignore[misc]
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

        # From skills - now using all available fields
        for skill in self.skills:
            # Skill name and description
            keywords.extend(skill.name.lower().split())
            keywords.extend(skill.description.lower().split())
            # Tags (already lowercase typically)
            keywords.extend([tag.lower() for tag in skill.tags])
            # Examples (extract key phrases from example prompts)
            for example in skill.examples:
                keywords.extend(example.lower().split())

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

            # Build cache using Pydantic validation
            self.cache = {}
            for agent_data in agents_data:
                agent_info = AgentInfo.model_validate(agent_data)
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
