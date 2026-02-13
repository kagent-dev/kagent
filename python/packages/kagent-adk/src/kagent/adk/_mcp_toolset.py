from __future__ import annotations

import asyncio
from typing import Optional

from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext
from google.adk.tools import BaseTool


def _enrich_cancelled_error(error: BaseException) -> asyncio.CancelledError:
    message = "Failed to create MCP session: operation cancelled"
    if str(error):
        message = f"{message}: {error}"
    return asyncio.CancelledError(message)


class KAgentMcpToolset(McpToolset):
    """McpToolset variant that catches and enriches errors during MCP session setup.

    This is particularly useful for explicitly catching and enriching failures that the base
    implementation may not catch and propagate without enough context.
    """

    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> list[BaseTool]:
        try:
            return await super().get_tools(readonly_context)
        except asyncio.CancelledError as error:
            raise _enrich_cancelled_error(error) from error
