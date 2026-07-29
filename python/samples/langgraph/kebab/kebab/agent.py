"""LangGraph kebab sample."""

from kagent.core import AsyncControllerClient, AsyncFileTokenProvider, KAgentConfig
from kagent.langgraph import KAgentCheckpointer
from langchain_core.tools import tool
from langchain_openai import ChatOpenAI
from langgraph.prebuilt import create_react_agent

kagent_config = KAgentConfig()
controller_client = AsyncControllerClient(
    kagent_config.grpc_url,
    agent_name=kagent_config.app_name,
    token_provider=AsyncFileTokenProvider(),
)
kagent_checkpointer = KAgentCheckpointer(
    client=controller_client,
    app_name=kagent_config.app_name,
)


@tool
def make_kebab(style: str = "mixed") -> str:
    """Pretend to make a kebab. Returns fixed JSON for demos and tests."""
    return '{"status": "ready", "style": "' + style + '", "note": "fake_e2e"}'


SYSTEM_INSTRUCTION = (
    "You are a helpful assistant. When the user wants a kebab, call make_kebab then answer briefly using its result."
)

graph = create_react_agent(
    model=ChatOpenAI(model="gpt-4o-mini"),
    tools=[make_kebab],
    checkpointer=kagent_checkpointer,
    prompt=SYSTEM_INSTRUCTION,
)
