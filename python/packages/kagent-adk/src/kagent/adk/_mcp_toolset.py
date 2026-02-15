from __future__ import annotations

import asyncio
import logging
from typing import Optional

from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext
from google.adk.tools import BaseTool

logger = logging.getLogger(__name__)


def _enrich_cancelled_error(error: BaseException) -> asyncio.CancelledError:
    message = "Failed to create MCP session: operation cancelled"
    if str(error):
        message = f"{message}: {error}"
    return asyncio.CancelledError(message)


def _should_require_confirmation(tool: BaseTool, confirm_config) -> bool:
    """Determine if a specific tool should require confirmation based on MCP annotations.

    When confirm_config is present, all tools default to requiring confirmation.
    Exception rules can opt-out specific tools:
    - except_read_only=True: skip if tool has readOnlyHint=True
    - except_idempotent=True: skip if tool has idempotentHint=True
    - except_non_destructive=True: skip if tool has destructiveHint=False
      (None defaults to True per MCP spec, so only explicit False skips)
    - except_tools: skip if tool name is in the list
    """
    if confirm_config is None:
        return False

    if confirm_config.except_tools and tool.name in confirm_config.except_tools:
        return False

    annotations = None
    if hasattr(tool, "_mcp_tool") and tool._mcp_tool is not None:
        annotations = getattr(tool._mcp_tool, "annotations", None)

    if annotations is not None:
        # MCP default for readOnlyHint is False, so None means not read-only
        if confirm_config.except_read_only and getattr(annotations, "readOnlyHint", None) is True:
            return False

        # MCP default for idempotentHint is False, so None means not idempotent
        if confirm_config.except_idempotent and getattr(annotations, "idempotentHint", None) is True:
            return False

        # MCP default for destructiveHint is True, so None means destructive — only explicit False skips
        if confirm_config.except_non_destructive and getattr(annotations, "destructiveHint", None) is False:
            return False

    return True


class KAgentMcpToolset(McpToolset):
    """McpToolset variant that catches and enriches errors during MCP session setup,
    and applies per-tool confirmation rules based on MCP annotations."""

    def __init__(self, confirm_config=None, **kwargs):
        super().__init__(**kwargs)
        self._confirm_config = confirm_config

    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> list[BaseTool]:
        try:
            tools = await super().get_tools(readonly_context)
        except asyncio.CancelledError as error:
            raise _enrich_cancelled_error(error) from error

        if self._confirm_config is not None:
            for tool in tools:
                should_confirm = _should_require_confirmation(tool, self._confirm_config)
                tool._require_confirmation = should_confirm

        return tools
