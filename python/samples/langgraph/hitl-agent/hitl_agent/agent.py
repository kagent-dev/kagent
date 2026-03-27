import logging
from datetime import datetime
from typing import Annotated, Any

import httpx
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentCheckpointer, KAgentHumanInTheLoopMiddleware
from langchain.agents import create_agent
from langchain_core.messages import AIMessage, ToolMessage
from langchain_core.tools import tool
from langchain_openai import ChatOpenAI
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from langgraph.types import interrupt
from typing_extensions import TypedDict

logger = logging.getLogger(__name__)

kagent_checkpointer = KAgentCheckpointer(
    client=httpx.AsyncClient(base_url=KAgentConfig().url),
    app_name=KAgentConfig().app_name,
)

# -- Tools -------------------------------------------------------------------


@tool
def get_time() -> str:
    """Get the current date and time. This is a safe tool that runs without approval."""
    return datetime.now().isoformat()


@tool
def delete_file(path: str) -> str:
    """Delete a file at the given path. This is a dangerous operation that requires human approval.

    Args:
        path: The file path to delete.
    """
    # In a real agent this would actually delete the file.
    # For this demo we just pretend.
    return f"File '{path}' has been deleted."


ALL_TOOLS = [get_time, delete_file]
TOOL_MAP = {t.name: t for t in ALL_TOOLS}

graph = create_agent(
    model=ChatOpenAI(model="openai-gpt-4"),
    tools=ALL_TOOLS,
    checkpointer=kagent_checkpointer,
    middleware=[
        KAgentHumanInTheLoopMiddleware(
            interrupt_on={"delete_file": True},
        )
    ],
)
