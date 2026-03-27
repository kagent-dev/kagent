from __future__ import annotations

import asyncio
import logging
from typing import Any, Dict, Optional

import httpx
from google.adk.tools import BaseTool
from google.adk.tools.mcp_tool.mcp_tool import McpTool
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext
from google.adk.tools.tool_context import ToolContext
from mcp.shared.exceptions import McpError

logger = logging.getLogger("kagent_adk." + __name__)

# Connection errors that indicate an unreachable MCP server.
# When these occur, the tool should return an error message to the LLM
# instead of raising, so the LLM can respond to the user rather than
# retrying the broken tool indefinitely.
#
# - ConnectionError: stdlib base for ConnectionResetError, ConnectionRefusedError, etc.
# - TimeoutError: stdlib timeout (e.g. socket.timeout)
# - httpx.TransportError: covers httpx.NetworkError (ConnectError, ReadError,
#   WriteError, CloseError), httpx.TimeoutException, httpx.ProtocolError, etc.
#   These do NOT inherit from stdlib ConnectionError/OSError.
# - McpError: raised by mcp.shared.session.send_request() when the underlying
#   SSE/HTTP stream drops or a tool call hits the session read timeout. The MCP
#   client wraps the transport-level error into McpError before it reaches us.
_CONNECTION_ERROR_TYPES = (
    ConnectionError,
    TimeoutError,
    httpx.TransportError,
    McpError,
)


def _enrich_cancelled_error(error: BaseException) -> asyncio.CancelledError:
    message = "Failed to create MCP session: operation cancelled"
    if str(error):
        message = f"{message}: {error}"
    return asyncio.CancelledError(message)


class ConnectionSafeMcpTool(McpTool):
    """McpTool wrapper that catches connection errors and returns them as
    error text to the LLM instead of raising.

    Without this, a persistent connection failure (e.g. "connection reset by
    peer") causes the LLM to retry the tool call in a tight loop, burning
    100% CPU for up to max_llm_calls iterations.

    See: https://github.com/kagent-dev/kagent/issues/1530
    """

    async def run_async(
        self,
        *,
        args: Dict[str, Any],
        tool_context: ToolContext,
    ) -> Dict[str, Any]:
        try:
            return await super().run_async(args=args, tool_context=tool_context)
        except _CONNECTION_ERROR_TYPES as error:
            error_message = (
                f"MCP tool '{self.name}' failed due to a connection error: "
                f"{type(error).__name__}: {error}. "
                "The MCP server may be unreachable. "
                "Do not retry this tool — inform the user about the failure."
            )
            logger.error(error_message)
            return {"error": error_message}


class KAgentMcpToolset(McpToolset):
    """McpToolset variant that catches and enriches errors during MCP session setup
    and handles cancel scope issues during cleanup.

    This is particularly useful for explicitly catching and enriching failures that the base
    implementation may not catch and propagate without enough context.
    """

    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> list[BaseTool]:
        try:
            tools = await super().get_tools(readonly_context)
        except asyncio.CancelledError as error:
            raise _enrich_cancelled_error(error) from error

        # Wrap each McpTool with ConnectionSafeMcpTool so that connection
        # errors are returned as error text instead of raised.
        # Uses __new__ + __dict__ copy to re-type the instance without calling
        # McpTool.__init__ (which requires connection params we don't have).
        # This is safe because McpTool uses plain instance attributes, not
        # __slots__ or descriptors.
        wrapped_tools: list[BaseTool] = []
        for tool in tools:
            if isinstance(tool, McpTool) and not isinstance(tool, ConnectionSafeMcpTool):
                safe_tool = ConnectionSafeMcpTool.__new__(ConnectionSafeMcpTool)
                safe_tool.__dict__.update(tool.__dict__)
                wrapped_tools.append(safe_tool)
            else:
                wrapped_tools.append(tool)
        return wrapped_tools

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
