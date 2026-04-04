"""ThinkTool — scratch pad for multi-step planning.

Lets the agent reason step-by-step before taking actions.
No side effects; result goes only into the agent's context window.
"""
from __future__ import annotations

from langchain_core.tools import tool


@tool
def think(thought: str) -> str:
    """Use this to reason step-by-step before taking actions.

    Write out your plan, assumptions, and what you need to do next.
    This is a scratch pad — no external calls are made.
    Use it for complex multi-step tasks before calling other tools.
    """
    return f"[Thought recorded]: {thought}"
