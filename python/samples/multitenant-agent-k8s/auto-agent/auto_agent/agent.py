"""Auto Agent — Phase 0.9 BYO LangGraph autonomous agent.

ReAct loop with dynamic tool discovery:
  1. think        — scratch pad for planning
  2. list_tools   — discover available tools from tool-registry
  3. call_tool    — execute any tool by name (scope-checked via registry)
  4. ask_human    — HITL: pause before destructive actions
  5. call_agent   — A2A delegation to other agents

Memory: KAgentCheckpointer → PostgreSQL (per context_id / thread_id).
"""
from __future__ import annotations

import logging
import os

import httpx
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentCheckpointer
from langchain_openai import ChatOpenAI
from langgraph.prebuilt import create_react_agent

from auto_agent.tools import (
    ask_human,
    call_agent,
    call_tool,
    list_available_tools,
    think,
)

log = logging.getLogger(__name__)

# ── Tenant identity ───────────────────────────────────────────────────────────
TENANT_ID = os.getenv("TENANT_ID", "unknown")
ORG_ID = os.getenv("ORG_ID", "unknown")
ORG_NAME = os.getenv("ORG_NAME", ORG_ID.replace("-", " ").title())

# ── System prompt ─────────────────────────────────────────────────────────────
SYSTEM_PROMPT = f"""You are an autonomous AI assistant for **{ORG_NAME}** \
(tenant: {TENANT_ID}, org: {ORG_ID}).

## Workflow — always follow this order:

1. **think** — plan complex multi-step tasks before acting
2. **list_available_tools** — discover what tools you can use
3. **ask_human** — REQUIRED before ANY create / update / delete action
4. **call_tool** — execute the tool with correct arguments
5. On errors: retry once with corrected args, then ask_human if still failing

## Rules:

- NEVER guess tool names — always call list_available_tools first
- NEVER perform destructive actions without ask_human confirmation
- NEVER fabricate tool results — only report what tools actually return
- Respond in the same language the user wrote in
- Keep responses concise — no markdown headers in final answer

## Memory:

You remember previous messages in this conversation via thread_id.
If the user refers to "earlier" or "before" — use that context.
"""

# ── LLM ───────────────────────────────────────────────────────────────────────
_model = os.getenv("OPENAI_MODEL", "gpt-4o-mini")
_base_url = os.getenv("OPENAI_BASE_URL")

_llm_kwargs: dict = {"model": _model, "temperature": 0}
if _base_url:
    _llm_kwargs["base_url"] = _base_url

log.info("LLM: model=%s base_url=%s", _model, _base_url or "(default openai)")

# ── KAgent memory ─────────────────────────────────────────────────────────────
_kagent_config = KAgentConfig()
kagent_checkpointer = KAgentCheckpointer(
    client=httpx.AsyncClient(base_url=_kagent_config.url),
    app_name=_kagent_config.app_name,
)

# ── Graph ─────────────────────────────────────────────────────────────────────
graph = create_react_agent(
    model=ChatOpenAI(**_llm_kwargs),
    tools=[think, list_available_tools, call_tool, ask_human, call_agent],
    checkpointer=kagent_checkpointer,
    prompt=SYSTEM_PROMPT,
)
