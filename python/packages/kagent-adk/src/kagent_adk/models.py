import logging
from typing import Literal, Self, Union

from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.agents.remote_a2a_agent import RemoteA2aAgent
from google.adk.agents.run_config import RunConfig, StreamingMode
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import MCPToolset, SseConnectionParams, StreamableHTTPConnectionParams
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)


class RemoteAgentConfig(BaseModel):
    name: str
    url: str
    description: str = ""


class BaseLLM(BaseModel):
    model: str


class OpenAI(BaseLLM):
    base_url: str | None = None

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
    http_tools: list[HttpMcpServerConfig] | None = None  # tools, always MCP
    sse_tools: list[SseMcpServerConfig] | None = None  # tools, always MCP
    remote_agents: list[RemoteAgentConfig] | None = None  # remote agents

    def to_agent(self, name: str) -> Agent:
        if name is None or not str(name).strip():
            raise ValueError("Agent name must be a non-empty string.")
        mcp_toolsets: list[ToolUnion] = []
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                mcp_toolsets.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add stdio tools
                mcp_toolsets.append(MCPToolset(connection_params=sse_tool.params, tool_filter=sse_tool.tools))
        remote_agents: list[BaseAgent] = []
        if self.remote_agents:
            for remote_agent in self.remote_agents:  # Add remote agents as tools
                remote_agents.append(
                    RemoteA2aAgent(
                        name=remote_agent.name,
                        agent_card=remote_agent.url,
                        description=remote_agent.description,
                    )
                )
        if self.model.type == "openai":
            model = LiteLlm(model=f"openai/{self.model.model}", base_url=self.model.base_url)
        elif self.model.type == "anthropic":
            model = LiteLlm(model=f"anthropic/{self.model.model}", base_url=self.model.base_url)
        elif self.model.type == "gemini_vertex_ai":
            model = GeminiLLM(model=self.model.model)
        elif self.model.type == "gemini_anthropic":
            model = ClaudeLLM(model=self.model.model)
        elif self.model.type == "ollama":
            model = LiteLlm(model=f"ollama_chat/{self.model.model}")
        elif self.model.type == "azure_openai":
            model = LiteLlm(model=f"azure/{self.model.model}")
        elif self.model.type == "gemini":
            model = self.model.model
        else:
            raise ValueError(f"Invalid model type: {self.model.type}")
        return Agent(
            name=name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=mcp_toolsets,
            sub_agents=remote_agents,
        )
