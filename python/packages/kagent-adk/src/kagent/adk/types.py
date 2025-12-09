import logging
from typing import Any, Literal, Optional, Union

import httpx
from agentsts.adk import ADKTokenPropagationPlugin
from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.agents.remote_a2a_agent import AGENT_CARD_WELL_KNOWN_PATH, DEFAULT_TIMEOUT, RemoteA2aAgent
from google.adk.code_executors.base_code_executor import BaseCodeExecutor
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import McpToolset, SseConnectionParams, StreamableHTTPConnectionParams
from pydantic import BaseModel, Field

from kagent.adk.sandbox_code_executer import SandboxedLocalCodeExecutor

from .models import AzureOpenAI as OpenAIAzure
from .models import OpenAI as OpenAINative

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
    headers: dict[str, Any] | None = None
    timeout: float = DEFAULT_TIMEOUT
    description: str = ""


class BaseLLM(BaseModel):
    model: str
    headers: dict[str, str] | None = None

    # TLS/SSL configuration (applies to all model types)
    tls_disable_verify: bool | None = None
    tls_ca_cert_path: str | None = None
    tls_disable_system_cas: bool | None = None


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
    execute_code: bool | None = None

    def to_agent(self, name: str, sts_integration: Optional[ADKTokenPropagationPlugin] = None) -> Agent:
        if name is None or not str(name).strip():
            raise ValueError("Agent name must be a non-empty string.")
        tools: list[ToolUnion] = []
        header_provider = None
        if sts_integration:
            header_provider = sts_integration.header_provider
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                tools.append(
                    McpToolset(
                        connection_params=http_tool.params, tool_filter=http_tool.tools, header_provider=header_provider
                    )
                )
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add sse tools
                tools.append(
                    McpToolset(
                        connection_params=sse_tool.params, tool_filter=sse_tool.tools, header_provider=header_provider
                    )
                )
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

                tools.append(AgentTool(agent=remote_a2a_agent))

        extra_headers = self.model.headers or {}

        code_executor = SandboxedLocalCodeExecutor() if self.execute_code else None

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
                # TLS configuration
                tls_disable_verify=self.model.tls_disable_verify,
                tls_ca_cert_path=self.model.tls_ca_cert_path,
                tls_disable_system_cas=self.model.tls_disable_system_cas,
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
            model = OpenAIAzure(
                model=self.model.model,
                type="azure_openai",
                default_headers=extra_headers,
                # TLS configuration
                tls_disable_verify=self.model.tls_disable_verify,
                tls_ca_cert_path=self.model.tls_ca_cert_path,
                tls_disable_system_cas=self.model.tls_disable_system_cas,
            )
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
            code_executor=code_executor,
        )
