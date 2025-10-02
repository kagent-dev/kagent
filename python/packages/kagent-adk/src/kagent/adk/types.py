import logging
from typing import Any, Literal, Union

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


def sanitize_agent_name(name: str, max_length: int = None) -> str:
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
        if name is None or not str(name).strip():
            raise ValueError("Agent name must be a non-empty string.")
        tools: list[ToolUnion] = []
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                tools.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add stdio tools
                tools.append(MCPToolset(connection_params=sse_tool.params, tool_filter=sse_tool.tools))
        if self.remote_agents:
            for remote_agent in self.remote_agents:  # Add remote agents as tools
                client = None

                if remote_agent.headers:
                    client = httpx.AsyncClient(
                        headers=remote_agent.headers, timeout=httpx.Timeout(timeout=remote_agent.timeout)
                    )

                remote_a2a_agent = RemoteA2aAgent(
                    name=remote_agent.name,
                    agent_card=f"{remote_agent.url}/{AGENT_CARD_WELL_KNOWN_PATH}",
                    description=remote_agent.description,
                    httpx_client=client,
                )

                tools.append(AgentTool(agent=remote_a2a_agent, skip_summarization=True))

        # Add workflow subagents as tools
        if self.workflow_subagents:
            for workflow in self.workflow_subagents:
                # Create remote agents for each subagent in the workflow
                tool_sub_agents: list[BaseAgent] = []
                for subagent in workflow.subagents:
                    client = None
                    if subagent.headers:
                        client = httpx.AsyncClient(
                            headers=subagent.headers, timeout=httpx.Timeout(timeout=subagent.timeout)
                        )

                    remote_agent = RemoteA2aAgent(
                        name=subagent.name,
                        agent_card=f"{subagent.url}/{AGENT_CARD_WELL_KNOWN_PATH}",
                        description=subagent.description,
                        httpx_client=client,
                    )
                    tool_sub_agents.append(remote_agent)

                # Create workflow agent based on type
                # Sanitize the role to create a valid agent name
                # OpenAI has a 64-character limit for tool names, so we need to ensure workflow names don't exceed it
                sanitized_role = sanitize_agent_name(workflow.role) if workflow.role else ""
                
                workflow_agent: BaseAgent
                if workflow.type == "Sequential":
                    workflow_name = f"{name}_{sanitized_role}_sequential" if sanitized_role else f"{name}_sequential"
                    workflow_name = sanitize_agent_name(workflow_name, max_length=64)
                    workflow_agent = SequentialAgent(
                        name=workflow_name,
                        sub_agents=tool_sub_agents,
                    )
                elif workflow.type == "Parallel":
                    workflow_name = f"{name}_{sanitized_role}_parallel" if sanitized_role else f"{name}_parallel"
                    workflow_name = sanitize_agent_name(workflow_name, max_length=64)
                    workflow_agent = ParallelAgent(
                        name=workflow_name,
                        sub_agents=tool_sub_agents,
                    )
                elif workflow.type == "Loop":
                    # LoopAgent automatically handles exit_loop() calls from tools
                    workflow_name = f"{name}_{sanitized_role}_loop" if sanitized_role else f"{name}_loop"
                    workflow_name = sanitize_agent_name(workflow_name, max_length=64)
                    workflow_agent = LoopAgent(
                        name=workflow_name,
                        sub_agents=tool_sub_agents,
                        max_iterations=workflow.max_iterations,
                    )
                else:
                    raise ValueError(f"Unknown workflow type: {workflow.type}")

                # Add workflow agent as a tool
                tools.append(AgentTool(agent=workflow_agent, skip_summarization=True))

        extra_headers = self.model.headers or {}

        if self.model.type == "openai":
            model = OpenAINative(
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
            )
        elif self.model.type == "anthropic":
            model = LiteLlm(
                model=f"anthropic/{self.model.model}", base_url=self.model.base_url, extra_headers=extra_headers
            )
        elif self.model.type == "gemini_vertex_ai":
            model = GeminiLLM(model=self.model.model)
        elif self.model.type == "gemini_anthropic":
            model = ClaudeLLM(model=self.model.model)
        elif self.model.type == "ollama":
            model = LiteLlm(model=f"ollama_chat/{self.model.model}", extra_headers=extra_headers)
        elif self.model.type == "azure_openai":
            model = OpenAIAzure(model=self.model.model, type="azure_openai", default_headers=extra_headers)
        elif self.model.type == "gemini":
            model = self.model.model
        else:
            raise ValueError(f"Invalid model type: {self.model.type}")
        return Agent(
            name=name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=tools,
        )
