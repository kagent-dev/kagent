from __future__ import annotations

import asyncio
import logging
from typing import Optional

from google.adk.tools import BaseTool
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext

logger = logging.getLogger("kagent_adk." + __name__)


def _enrich_cancelled_error(error: BaseException) -> asyncio.CancelledError:
    message = "Failed to create MCP session: operation cancelled"
    if str(error):
        message = f"{message}: {error}"
    return asyncio.CancelledError(message)


class KAgentMcpToolset(McpToolset):
    """McpToolset variant that catches and enriches errors during MCP session setup
    and handles cancel scope issues during cleanup.

    This is particularly useful for explicitly catching and enriching failures that the base
    implementation may not catch and propagate without enough context.
    """

    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> list[BaseTool]:
        try:
            return await super().get_tools(readonly_context)
        except asyncio.CancelledError as error:
            raise _enrich_cancelled_error(error) from error

    async def close(self) -> None:
        """Close MCP sessions and suppress known anyio cancel scope cleanup errors.

        We intentionally do not suppress arbitrary exceptions to avoid hiding
        unrelated cleanup failures.

        See: https://github.com/kagent-dev/kagent/issues/1276
        """
        try:
            await super().close()
        except BaseException as e:
            if is_anyio_cross_task_cancel_scope_error(e):
                logger.warning(
                    "Non-fatal anyio cancel scope error during MCP cleanup: %s: %s",
                    type(e).__name__,
                    e,
                )
                return
            if isinstance(e, (KeyboardInterrupt, SystemExit)):
                raise
            if isinstance(e, asyncio.CancelledError):
                raise
            raise


def is_anyio_cross_task_cancel_scope_error(error: BaseException) -> bool:
    current: BaseException | None = error
    seen: set[int] = set()
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        if isinstance(current, (RuntimeError, asyncio.CancelledError)):
            msg = str(current).lower()
            if "cancel scope" in msg and "different task" in msg:
                return True
        current = current.__cause__ or current.__context__
    return False
