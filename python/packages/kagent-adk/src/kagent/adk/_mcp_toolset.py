from __future__ import annotations

import asyncio
import logging
from typing import Optional

from google.adk.tools import BaseTool
from google.adk.tools.mcp_tool.mcp_session_manager import MCPSessionManager
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext
from mcp import ClientSession
from mcp.shared.exceptions import McpError

logger = logging.getLogger("kagent_adk." + __name__)

_PING_TIMEOUT_SECONDS = 2.0
_JSONRPC_METHOD_NOT_FOUND = -32601


def _is_server_alive_error(exc: Exception) -> bool:
    """Return True if the error proves the server is reachable.

    Some MCP servers don't implement the optional ``ping`` method and
    reply with JSON-RPC "Method not found" (-32601).  This still means
    the session is valid and the server is responding.
    """
    if isinstance(exc, McpError):
        return exc.error.code == _JSONRPC_METHOD_NOT_FOUND
    return False


class KAgentMCPSessionManager(MCPSessionManager):
    """Session manager that validates cached sessions via ping before reuse.

    The upstream ``MCPSessionManager`` checks ``_read_stream._closed`` /
    ``_write_stream._closed`` to decide whether a cached session is still
    usable.  Those are in-memory anyio channels that stay open even when
    the remote MCP server restarts, so the check always passes and the
    stale ``mcp-session-id`` is sent to the new server, which replies
    with HTTP 404 → ``"Session terminated"``.

    This subclass adds a lightweight ``send_ping()`` probe after the
    parent returns a cached session.  If the ping fails the cached
    session is torn down and a brand-new one is created transparently.
    """

    async def create_session(self, headers: dict[str, str] | None = None) -> ClientSession:
        session = await super().create_session(headers)

        try:
            await asyncio.wait_for(session.send_ping(), timeout=_PING_TIMEOUT_SECONDS)
            return session
        except Exception as exc:
            if _is_server_alive_error(exc):
                return session
            logger.warning("MCP session failed ping validation, invalidating cached session and creating a fresh one")
            try:
                await self.close()
            except Exception as close_exc:
                logger.debug("Non-fatal error while closing stale session: %s", close_exc)
            return await super().create_session(headers)


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

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._mcp_session_manager = KAgentMCPSessionManager(
            connection_params=self._connection_params,
            errlog=self._errlog,
        )

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
