import json
import logging
from typing import Literal, Self, Union

import aiofiles
from a2a.types import AgentCard
from google.adk.agents import Agent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import MCPToolset, SseConnectionParams, StreamableHTTPConnectionParams
from google.genai import types  # For creating message Content/Parts
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)


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


class AgentConfig(BaseModel):
    kagent_url: str  # The URL of the KAgent server
    agent_card: AgentCard
    name: str
    model: Union[OpenAI, Anthropic, GeminiVertexAI, GeminiAnthropic, Ollama, AzureOpenAI] = Field(discriminator="type")
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None  # tools, always MCP
    sse_tools: list[SseMcpServerConfig] | None = None  # tools, always MCP
    agents: list[Self] | None = None  # agent names

    def to_agent(self) -> Agent:
        mcp_toolsets: list[ToolUnion] = []
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                mcp_toolsets.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add stdio tools
                mcp_toolsets.append(MCPToolset(connection_params=sse_tool.params, tool_filter=sse_tool.tools))
        if self.agents:
            for agent in self.agents:  # Add sub agents as tools
                mcp_toolsets.append(AgentTool(agent.to_agent()))
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
        else:
            raise ValueError(f"Invalid model type: {self.model.type}")
        return Agent(
            name=self.name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=mcp_toolsets,
        )


async def test_agent(filepath: str, task: str):
    async with aiofiles.open(filepath, "r") as f:
        content = await f.read()
        config = json.loads(content)
    agent_config = AgentConfig.model_validate(config)
    root_agent = agent_config.to_agent()

    session_service = InMemorySessionService()
    SESSION_ID = "12345"
    USER_ID = "admin"
    await session_service.create_session(
        app_name=agent_config.name,
        session_id=SESSION_ID,
        user_id=USER_ID,
    )

    runner = Runner(
        agent=root_agent,
        app_name=agent_config.name,
        session_service=session_service,
    )

    logger.info(f"\n>>> User Query: {task}")

    # Prepare the user's message in ADK format
    content = types.Content(role="user", parts=[types.Part(text=task)])
    # Key Concept: run_async executes the agent logic and yields Events.
    # We iterate through events to find the final answer.
    async for event in runner.run_async(user_id=USER_ID, session_id=SESSION_ID, new_message=content):
        # You can uncomment the line below to see *all* events during execution
        # print(f"  [Event] Author: {event.author}, Type: {type(event).__name__}, Final: {event.is_final_response()}, Content: {event.content}")

        # Key Concept: is_final_response() marks the concluding message for the turn.
        jsn = event.model_dump_json()
        logger.info(f"  [Event] {jsn}")
