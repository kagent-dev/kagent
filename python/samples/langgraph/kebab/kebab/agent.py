"""LangGraph kebab sample."""

import os
import sqlite3

from langchain_core.tools import tool
from langchain_openai import ChatOpenAI
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.prebuilt import create_react_agent

checkpointer = SqliteSaver(
    sqlite3.connect(os.getenv("KAGENT_CHECKPOINT_DB", "/tmp/kebab-checkpoints.sqlite"), check_same_thread=False)
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
    checkpointer=checkpointer,
    prompt=SYSTEM_INSTRUCTION,
)
