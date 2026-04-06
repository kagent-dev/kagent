"""CallToolTool — execute any available tool by name.

Proxies through tool-registry which enforces tenant+org scope check.
Hard timeout: 30 s (configured in registry_client).
"""
from __future__ import annotations

import os

from langchain_core.tools import tool

from ..registry_client import call_tool as _call


@tool
async def call_tool(tool_name: str, arguments: dict) -> dict:
    """Call an available tool by name with the given arguments.

    Always verify the tool exists via list_available_tools before calling.
    For create/update/delete actions — ask the user for confirmation first
    using ask_human.

    Args:
        tool_name: exact tool name from list_available_tools
        arguments: dict matching the tool's input_schema
    """
    result = await _call(
        tool_name=tool_name,
        arguments=arguments,
        tenant_id=os.getenv("TENANT_ID", "unknown"),
        org_id=os.getenv("ORG_ID", "unknown"),
    )

    if "error" in result:
        return {"status": "error", "detail": result["error"]}

    return result
