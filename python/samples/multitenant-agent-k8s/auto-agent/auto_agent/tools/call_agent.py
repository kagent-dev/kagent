"""CallAgentTool — delegate a task to another agent via A2A (kagent).

Calls the kagent-controller A2A endpoint asynchronously.
Use for cross-org sub-tasks (e.g. management-agent → sales-agent).
"""
from __future__ import annotations

import logging
import os

import httpx
from langchain_core.tools import tool

log = logging.getLogger(__name__)

KAGENT_URL = os.getenv(
    "KAGENT_URL",
    "http://kagent-controller.kagent.svc.cluster.local:8083",
)
_A2A_TIMEOUT = 90  # seconds — LLM calls can be slow


@tool
async def call_agent(agent_name: str, message: str, namespace: str = "") -> str:
    """Delegate a task to another agent by name using A2A protocol.

    Use when you need a specialist agent to handle part of the task
    (e.g. call sales-agent for CRM operations).

    Args:
        agent_name: name of the target agent (e.g. "sales-agent")
        message:    task description to send to the agent
        namespace:  K8s namespace (default: current tenant namespace)
    """
    tenant_id = os.getenv("TENANT_ID", "unknown")
    ns = namespace or f"tenant-{tenant_id}"
    # URL-encode path segments to prevent path traversal
    safe_ns = ns.replace("/", "%2F")
    safe_name = agent_name.replace("/", "%2F")
    url = f"{KAGENT_URL}/api/a2a/{safe_ns}/{safe_name}/"

    body = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "message/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": message}],
            }
        },
    }

    try:
        async with httpx.AsyncClient(timeout=_A2A_TIMEOUT) as client:
            r = await client.post(url, json=body)
            r.raise_for_status()
            data = r.json()

        # Extract text from A2A response
        result = data.get("result", {})
        parts = result.get("parts", []) or result.get("message", {}).get("parts", [])
        texts = [p.get("text", "") for p in parts if p.get("kind") == "text"]
        return "\n".join(texts) if texts else str(result)

    except httpx.TimeoutException:
        return f"[error] Agent '{agent_name}' timed out after {_A2A_TIMEOUT}s"
    except Exception as exc:
        log.error("call_agent(%s) failed: %s", agent_name, exc)
        return f"[error] {exc}"
