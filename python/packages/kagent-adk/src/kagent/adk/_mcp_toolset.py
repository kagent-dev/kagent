from __future__ import annotations

import asyncio
import logging
from typing import Any, Optional

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
#
# McpError is handled separately in ConnectionSafeMcpTool.run_async() because
# it is the general MCP protocol error class. Only transport-level McpErrors
# (e.g., session read timeouts) should be caught; protocol-level McpErrors
# (e.g., invalid tool arguments) must propagate so the LLM can correct itself.
_CONNECTION_ERROR_TYPES = (
    ConnectionError,
    TimeoutError,
    httpx.TransportError,
)

# Keywords in McpError messages that indicate transport-level failures
# (as opposed to protocol-level errors like invalid arguments).
_TRANSPORT_MCP_ERROR_KEYWORDS = (
    "timeout", "timed out", "connection", "eof", "reset",
    "closed", "transport", "stream", "unreachable",
)


def _is_transport_mcp_error(error: McpError) -> bool:
    """Check if an McpError represents a transport-level failure.

    McpError wraps all MCP protocol errors, but only transport-level failures
    (e.g., session read timeouts, stream closures) should be caught and
    returned to the LLM as non-retryable errors. Protocol-level errors
    (e.g., invalid tool arguments, server validation failures) should
    propagate so the LLM can correct its behavior.
    """
    message = error.error.message.lower()
    return any(keyword in message for keyword in _TRANSPORT_MCP_ERROR_KEYWORDS)


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

    Uses composition: delegates to an inner McpTool instance via __getattr__,
    avoiding the fragile __new__ + __dict__ copy pattern that would break if
    upstream McpTool adds __slots__, properties, or post-init hooks.

    See: https://github.com/kagent-dev/kagent/issues/1530
    """

    _inner_tool: McpTool

    def __init__(self, inner_tool: McpTool):
        # Store the inner tool without calling McpTool.__init__
        # (which requires connection params we don't have).
        object.__setattr__(self, "_inner_tool", inner_tool)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._inner_tool, name)

    def _connection_error_response(self, error: Exception) -> dict[str, Any]:
        error_message = (
            f"MCP tool '{self.name}' failed due to a connection error: "
            f"{type(error).__name__}: {error}. "
            "The MCP server may be unreachable. "
            "Do not retry this tool — inform the user about the failure."
        )
        logger.error(error_message, exc_info=error)
        return {"error": error_message}

    async def run_async(
        self,
        *,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> dict[str, Any]:
        try:
            return await self._inner_tool.run_async(args=args, tool_context=tool_context)
        except _CONNECTION_ERROR_TYPES as error:
            return self._connection_error_response(error)
        except McpError as error:
            if not _is_transport_mcp_error(error):
                raise
            return self._connection_error_response(error)


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
        wrapped_tools: list[BaseTool] = []
        for tool in tools:
            if isinstance(tool, McpTool) and not isinstance(tool, ConnectionSafeMcpTool):
                wrapped_tools.append(ConnectionSafeMcpTool(tool))
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
