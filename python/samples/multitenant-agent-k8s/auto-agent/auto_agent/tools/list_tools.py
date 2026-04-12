"""ListToolsTool — dynamic tool discovery from tool-registry.

Returns name + description + input_schema for all tools available
to the current org. Results are TTL-cached (60 s) in registry_client.
"""
from __future__ import annotations

import os

from langchain_core.tools import tool

from ..registry_client import get_tools


@tool
async def list_available_tools(category: str = "") -> list[dict]:
    """List all tools available to you right now.

    Returns name, description, and input_schema for each tool.
    Call this when you need to accomplish a task and are unsure which tools exist.
    Results are cached — no cost to call multiple times.

    Args:
        category: optional filter keyword (e.g. "crm", "search"). Leave empty for all.
    """
    tenant_id = os.getenv("TENANT_ID", "unknown")
    org_id = os.getenv("ORG_ID", "unknown")
    tools = await get_tools(tenant_id=tenant_id, org_id=org_id)

    result = []
    for t in tools:
        if category and category.lower() not in t.name.lower() and category.lower() not in t.description.lower():
            continue
        result.append({
            "name": t.name,
            "description": t.description,
            "input_schema": t.input_schema,
        })

    if not result:
        return [{"info": "No tools available" + (f" matching '{category}'" if category else "") + ". Try without filter."}]

    return result
