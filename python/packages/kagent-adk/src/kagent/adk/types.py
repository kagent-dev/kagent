import logging
from typing import Any, Literal, Optional, Union

import httpx
from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.agents.remote_a2a_agent import AGENT_CARD_WELL_KNOWN_PATH, DEFAULT_TIMEOUT, RemoteA2aAgent
from google.adk.agents import SequentialAgent, LoopAgent, ParallelAgent
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import MCPToolset, SseConnectionParams, StreamableHTTPConnectionParams
from pydantic import BaseModel, Field

from .models import AzureOpenAI as OpenAIAzure
from .models import OpenAI as OpenAINative

logger = logging.getLogger(__name__)


def sanitize_agent_name(name: str, max_length: Optional[int] = None) -> str:
    """
    Sanitize a string to be a valid agent name.
    Agent names must start with a letter or underscore and contain only letters, digits, and underscores.
    
    Args:
        name: The name to sanitize
        max_length: Optional maximum length for the sanitized name (e.g., 64 for OpenAI tool names)
    
    Returns:
        Sanitized name, truncated to max_length if specified
    """
    # Replace spaces and hyphens with underscores
    sanitized = name.replace(" ", "_").replace("-", "_")
    # Remove any other invalid characters
    sanitized = "".join(c for c in sanitized if c.isalnum() or c == "_")
    # Ensure it starts with a letter or underscore
    if sanitized and not (sanitized[0].isalpha() or sanitized[0] == "_"):
        sanitized = "_" + sanitized
    if not sanitized:
        sanitized = "_"
    
    # Truncate if max_length is specified
    if max_length and len(sanitized) > max_length:
        sanitized = sanitized[:max_length]
    
    return sanitized


def generate_workflow_name(base_name: str, sanitized_role: str, workflow_type: str) -> str:
    """Generate a workflow agent name with proper sanitization.
    
    Args:
        base_name: Base name for the workflow
        sanitized_role: Sanitized role string (already processed through sanitize_agent_name)
        workflow_type: Type of workflow (Sequential, Parallel, Loop)
    
    Returns:
        Sanitized workflow name with 64-character limit for OpenAI compatibility
    """
    suffix = workflow_type.lower()
    if sanitized_role:
        workflow_name = f"{base_name}_{sanitized_role}_{suffix}"
    else:
        workflow_name = f"{base_name}_{suffix}"
    return sanitize_agent_name(workflow_name, max_length=64)


def create_workflow_agent(
    workflow_type: str, agent_name: str, subagents: list[BaseAgent], max_iterations: int = 5
) -> BaseAgent:
    """Factory method to create workflow agents based on type.
    
    Args:
        workflow_type: Type of workflow ("Sequential", "Parallel", or "Loop")
        agent_name: Name for the workflow agent
        subagents: List of subagents to include in the workflow
        max_iterations: Maximum iterations for Loop workflows (default: 5)
    
    Returns:
        The created workflow agent instance
    
    Raises:
        ValueError: If workflow_type is not recognized
    """
    workflow_factories = {
        "Sequential": lambda: SequentialAgent(name=agent_name, sub_agents=subagents),
        "Parallel": lambda: ParallelAgent(name=agent_name, sub_agents=subagents),
        "Loop": lambda: LoopAgent(name=agent_name, sub_agents=subagents, max_iterations=max_iterations),
    }
    
    factory = workflow_factories.get(workflow_type)
    if factory is None:
        raise ValueError(f"Unknown workflow type: {workflow_type}")
    
    return factory()


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
        headers: Optional HTTP headers
        timeout: Request timeout in seconds
        description: Agent description
    
    Returns:
        Configured RemoteA2aAgent instance
    """
    client = None
    if headers:
        client = httpx.AsyncClient(
            headers=headers,
            timeout=httpx.Timeout(timeout=timeout)
        )
    
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


class SubagentConfig(BaseModel):
    """Configuration for a subagent in a workflow."""

    name: str
    url: str
    headers: dict[str, Any] | None = None
    timeout: float = DEFAULT_TIMEOUT
    description: str = ""


class WorkflowConfig(BaseModel):
    """Configuration for workflow subagents (Sequential, Parallel, or Loop)."""

    type: Literal["Sequential", "Parallel", "Loop"]
    subagents: list[SubagentConfig]
    role: str = ""
    max_iterations: int = 5  # For Loop agents


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


class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, GeminiVertexAI, GeminiAnthropic, Ollama, AzureOpenAI, Gemini] = Field(
        discriminator="type"
    )
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None  # Streamable HTTP MCP tools
    sse_tools: list[SseMcpServerConfig] | None = None  # SSE MCP tools
    remote_agents: list[RemoteAgentConfig] | None = None  # remote agents
    workflow_subagents: list[WorkflowConfig] | None = None  # workflow patterns (Sequential, Parallel, Loop)

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
        tools.extend(self._create_workflow_tools(name))
        return tools
    
    def _create_mcp_tools(self) -> list[ToolUnion]:
        """Create MCP toolsets from HTTP and SSE configurations.
        
        Returns:
            List of MCP toolsets
        """
        tools: list[ToolUnion] = []
        
        if self.http_tools:
            for http_tool in self.http_tools:
                tools.append(MCPToolset(
                    connection_params=http_tool.params,
                    tool_filter=http_tool.tools
                ))
        
        if self.sse_tools:
            for sse_tool in self.sse_tools:
                tools.append(MCPToolset(
                    connection_params=sse_tool.params,
                    tool_filter=sse_tool.tools
                ))
        
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
    
    def _create_workflow_tools(self, base_name: str) -> list[ToolUnion]:
        """Create workflow agent tools.
        
        Args:
            base_name: Base name for workflow agent naming
            
        Returns:
            List of AgentTools wrapping workflow agents
        """
        tools: list[ToolUnion] = []
        
        if self.workflow_subagents:
            for workflow in self.workflow_subagents:
                # Create remote agents for each subagent in the workflow
                tool_sub_agents = [
                    create_remote_agent(
                        name=subagent.name,
                        url=subagent.url,
                        headers=subagent.headers,
                        timeout=subagent.timeout,
                        description=subagent.description,
                    )
                    for subagent in workflow.subagents
                ]
                
                # Create workflow agent based on type
                sanitized_role = sanitize_agent_name(workflow.role) if workflow.role else ""
                workflow_name = generate_workflow_name(base_name, sanitized_role, workflow.type)
                
                workflow_agent = create_workflow_agent(
                    workflow_type=workflow.type,
                    agent_name=workflow_name,
                    subagents=tool_sub_agents,
                    max_iterations=workflow.max_iterations,
                )
                
                tools.append(AgentTool(agent=workflow_agent, skip_summarization=True))
        
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
            "anthropic": lambda: LiteLlm(
                model=f"anthropic/{self.model.model}",
                base_url=self.model.base_url,
                extra_headers=extra_headers
            ),
            "gemini_vertex_ai": lambda: GeminiLLM(model=self.model.model),
            "gemini_anthropic": lambda: ClaudeLLM(model=self.model.model),
            "ollama": lambda: LiteLlm(
                model=f"ollama_chat/{self.model.model}",
                extra_headers=extra_headers
            ),
            "azure_openai": lambda: OpenAIAzure(
                model=self.model.model,
                type="azure_openai",
                default_headers=extra_headers
            ),
            "gemini": lambda: self.model.model,
        }
        
        factory = model_factories.get(self.model.type)
        if factory is None:
            raise ValueError(f"Invalid model type: {self.model.type}")
        
        return factory()
