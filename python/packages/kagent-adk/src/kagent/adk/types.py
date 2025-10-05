import logging
from typing import Any, Literal, Union

import httpx
from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.agents.remote_a2a_agent import AGENT_CARD_WELL_KNOWN_PATH, DEFAULT_TIMEOUT, RemoteA2aAgent
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import MCPToolset, SseConnectionParams, StreamableHTTPConnectionParams
from pydantic import BaseModel, Field

from .models import AzureOpenAI as OpenAIAzure
from .models import OpenAI as OpenAINative

logger = logging.getLogger(__name__)


def create_remote_agent(
    name: str,
    url: str,
    headers: dict[str, Any] | None,
    timeout: float,
    description: str,
) -> RemoteA2aAgent:
    """Create a RemoteA2aAgent with optional HTTP client.

    Args:
        name: Agent name
        url: Agent base URL
        headers: Optional HTTP headers (from headersFrom config)
        timeout: Request timeout in seconds
        description: Agent description

    Returns:
        Configured RemoteA2aAgent instance with automatic user ID propagation
    """

    client = None
    if headers:
        client = httpx.AsyncClient(headers=headers, timeout=httpx.Timeout(timeout=timeout))

    return RemoteA2aAgent(
        name=name,
        agent_card=f"{url}/{AGENT_CARD_WELL_KNOWN_PATH}",
        description=description,
        httpx_client=client,
    )


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)


class RemoteAgentConfig(BaseModel):
    name: str
    url: str
    headers: dict[str, Any] | None = None
    timeout: float = DEFAULT_TIMEOUT
    description: str = ""


class BaseLLM(BaseModel):
    model: str
    headers: dict[str, str] | None = None


class OpenAI(BaseLLM):
    base_url: str | None = None
    frequency_penalty: float | None = None
    max_tokens: int | None = None
    n: int | None = None
    presence_penalty: float | None = None
    reasoning_effort: str | None = None
    seed: int | None = None
    temperature: float | None = None
    timeout: int | None = None
    top_p: float | None = None

    type: Literal["openai"]


class AzureOpenAI(BaseLLM):
    type: Literal["azure_openai"]


class Anthropic(BaseLLM):
    base_url: str | None = None

    type: Literal["anthropic"]


class GeminiVertexAI(BaseLLM):
    type: Literal["gemini_vertex_ai"]


class GeminiAnthropic(BaseLLM):
    type: Literal["gemini_anthropic"]


class Ollama(BaseLLM):
    type: Literal["ollama"]


class Gemini(BaseLLM):
    type: Literal["gemini"]


class SubAgentReference(BaseModel):
    """Reference to a sub-agent in a workflow."""

    name: str
    namespace: str = "default"
    kind: str = "Agent"
    description: str = ""


class WorkflowAgentConfig(BaseModel):
    """Configuration for workflow agents (Parallel, Sequential, Loop)."""

    name: str
    description: str
    namespace: str = "default"
    workflow_type: Literal["parallel", "sequential", "loop"]
    sub_agents: list[SubAgentReference]
    max_workers: int | None = None  # For parallel agents
    timeout: str | None = None  # For parallel agents (e.g., "5m")
    max_iterations: int | None = None  # For loop agents

    def to_agent(self) -> BaseAgent:
        """Convert workflow config to BaseAgent instance.

        Creates the appropriate workflow agent type (Parallel, Sequential, or Loop)
        with resolved sub-agents.

        Returns:
            BaseAgent: The workflow agent instance

        Raises:
            ValueError: If workflow_type is invalid or sub_agents is empty
        """
        if not self.sub_agents:
            raise ValueError("Workflow agent must have at least one sub-agent")

        # Convert sub-agent references to RemoteA2aAgent instances
        sub_agent_instances = []
        for sub_agent_ref in self.sub_agents:
            # Construct the agent URL (assumes standard KAgent deployment)
            agent_url = f"http://{sub_agent_ref.name}.{sub_agent_ref.namespace}:8080"

            # Create RemoteA2aAgent instance
            remote_agent = RemoteA2aAgent(
                name=sub_agent_ref.name.replace("-", "_"),  # Python identifier
                agent_card=f"{agent_url}/{AGENT_CARD_WELL_KNOWN_PATH}",
                description=sub_agent_ref.description,
            )
            sub_agent_instances.append(remote_agent)

        # Create the appropriate workflow agent type
        if self.workflow_type == "parallel":
            from .agents.parallel import KAgentParallelAgent

            return KAgentParallelAgent(
                name=self.name.replace("-", "_"),
                description=self.description,
                sub_agents=sub_agent_instances,
                max_workers=self.max_workers or 10,
                namespace=self.namespace,
            )
        elif self.workflow_type == "sequential":
            from google.adk.agents import SequentialAgent

            return SequentialAgent(
                name=self.name.replace("-", "_"),
                description=self.description,
                sub_agents=sub_agent_instances,
            )
        elif self.workflow_type == "loop":
            from google.adk.agents import LoopAgent

            if self.max_iterations is None:
                raise ValueError("Loop agent requires max_iterations")

            return LoopAgent(
                name=self.name.replace("-", "_"),
                description=self.description,
                sub_agents=sub_agent_instances,
                max_iterations=self.max_iterations,
            )
        else:
            raise ValueError(f"Unknown workflow type: {self.workflow_type}")


class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, GeminiVertexAI, GeminiAnthropic, Ollama, AzureOpenAI, Gemini] = Field(
        discriminator="type"
    )
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None  # Streamable HTTP MCP tools
    sse_tools: list[SseMcpServerConfig] | None = None  # SSE MCP tools
    remote_agents: list[RemoteAgentConfig] | None = None  # remote agents

    def to_agent(self, name: str) -> Agent:
        """Create an Agent instance from this configuration.

        Args:
            name: Name for the agent

        Returns:
            Configured Agent instance

        Raises:
            ValueError: If name is empty or invalid
        """
        if name is None or not str(name).strip():
            raise ValueError("Agent name must be a non-empty string.")

        tools = self._build_tools(name)
        model = self._create_model()

        return Agent(
            name=name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=tools,
        )

    def _build_tools(self, name: str) -> list[ToolUnion]:
        """Build all tools from configuration.

        Args:
            name: Base name for workflow tools

        Returns:
            List of all configured tools
        """
        tools: list[ToolUnion] = []
        tools.extend(self._create_mcp_tools())
        tools.extend(self._create_remote_agent_tools())
        return tools

    def _create_mcp_tools(self) -> list[ToolUnion]:
        """Create MCP toolsets from HTTP and SSE configurations.

        Returns:
            List of MCP toolsets
        """
        tools: list[ToolUnion] = []

        if self.http_tools:
            for http_tool in self.http_tools:
                tools.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))

        if self.sse_tools:
            for sse_tool in self.sse_tools:
                tools.append(MCPToolset(connection_params=sse_tool.params, tool_filter=sse_tool.tools))

        return tools

    def _create_remote_agent_tools(self) -> list[ToolUnion]:
        """Create tools from remote agent configurations.

        Returns:
            List of AgentTools wrapping remote agents
        """
        tools: list[ToolUnion] = []

        if self.remote_agents:
            for remote_agent in self.remote_agents:
                remote_a2a_agent = create_remote_agent(
                    name=remote_agent.name,
                    url=remote_agent.url,
                    headers=remote_agent.headers,
                    timeout=remote_agent.timeout,
                    description=remote_agent.description,
                )
                tools.append(AgentTool(agent=remote_a2a_agent, skip_summarization=True))

        return tools

    def _create_model(self):
        """Create the appropriate LLM model based on configuration.

        Returns:
            Configured LLM model instance

        Raises:
            ValueError: If model type is invalid
        """
        extra_headers = self.model.headers or {}

        # Factory pattern for model creation
        model_factories = {
            "openai": lambda: OpenAINative(
                type="openai",
                base_url=self.model.base_url,
                default_headers=extra_headers,
                frequency_penalty=self.model.frequency_penalty,
                max_tokens=self.model.max_tokens,
                model=self.model.model,
                n=self.model.n,
                presence_penalty=self.model.presence_penalty,
                reasoning_effort=self.model.reasoning_effort,
                seed=self.model.seed,
                temperature=self.model.temperature,
                timeout=self.model.timeout,
                top_p=self.model.top_p,
            ),
            "azure_openai": lambda: OpenAIAzure(
                model=self.model.model, type="azure_openai", default_headers=extra_headers
            ),
            "anthropic": lambda: LiteLlm(
                model=f"anthropic/{self.model.model}", base_url=self.model.base_url, extra_headers=extra_headers
            ),
            "gemini_vertex_ai": lambda: GeminiLLM(model=self.model.model),
            "gemini_anthropic": lambda: ClaudeLLM(model=self.model.model),
            "ollama": lambda: LiteLlm(model=f"ollama_chat/{self.model.model}", extra_headers=extra_headers),
            "gemini": lambda: self.model.model,
        }

        factory = model_factories.get(self.model.type)
        if factory is None:
            raise ValueError(f"Invalid model type: {self.model.type}")

        return factory()
