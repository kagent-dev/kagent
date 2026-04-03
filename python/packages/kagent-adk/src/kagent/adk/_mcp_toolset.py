from __future__ import annotations

import asyncio
import hashlib
import logging
import re
from typing import Any, Optional
from urllib.parse import urlparse

from google.adk.tools import BaseTool
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset, ReadonlyContext

from ._constants import PROXY_HOST_HEADER
from ._mcp_capability_tools import LoadKAgentMcpPromptTool, LoadKAgentMcpResourceTool

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
            tools = await super().get_tools(readonly_context)
        except asyncio.CancelledError as error:
            raise _enrich_cancelled_error(error) from error

        tools.extend(await self._create_capability_tools(readonly_context))
        return tools

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

    async def list_prompt_info(self, readonly_context: Optional[ReadonlyContext] = None) -> list[dict[str, Any]]:
        """Return prompt metadata exposed by the MCP server."""

        result = await self._execute_with_session(
            lambda session: session.list_prompts(),
            "Failed to list prompts from MCP server",
            readonly_context,
        )
        return [_model_dump(prompt) for prompt in result.prompts]

    async def get_prompt(
        self,
        name: str,
        arguments: dict[str, str] | None = None,
        readonly_context: Optional[ReadonlyContext] = None,
    ) -> dict[str, Any]:
        """Fetch prompt contents from the MCP server."""

        result = await self._execute_with_session(
            lambda session: session.get_prompt(name, arguments=arguments or None),
            f"Failed to get prompt {name} from MCP server",
            readonly_context,
        )
        return _model_dump(result)

    async def read_resource(
        self,
        name: str,
        readonly_context: Optional[ReadonlyContext] = None,
    ) -> list[dict[str, Any]]:
        """Fetch resource contents from the MCP server."""

        resource_info = await self.get_resource_info(name, readonly_context)
        if "uri" not in resource_info:
            raise ValueError(f"Resource '{name}' has no URI.")

        result = await self._execute_with_session(
            lambda session: session.read_resource(uri=resource_info["uri"]),
            f"Failed to get resource {name} from MCP server",
            readonly_context,
        )
        return [_model_dump(content) for content in result.contents]

    async def _create_capability_tools(self, readonly_context: Optional[ReadonlyContext]) -> list[BaseTool]:
        capability_tools: list[BaseTool] = []

        try:
            if await self._has_resources(readonly_context):
                capability_tools.append(
                    LoadKAgentMcpResourceTool(
                        mcp_toolset=self,
                        name=self.resource_tool_name,
                        server_label=self.server_label,
                    )
                )
        except Exception as error:
            logger.info("Skipping MCP resource helper tool: %s", error)

        try:
            if await self._has_prompts(readonly_context):
                capability_tools.append(
                    LoadKAgentMcpPromptTool(
                        mcp_toolset=self,
                        name=self.prompt_tool_name,
                        server_label=self.server_label,
                    )
                )
        except Exception as error:
            logger.info("Skipping MCP prompt helper tool: %s", error)

        return capability_tools

    async def _has_resources(self, readonly_context: Optional[ReadonlyContext]) -> bool:
        return bool(await self.list_resources(readonly_context))

    async def _has_prompts(self, readonly_context: Optional[ReadonlyContext]) -> bool:
        return bool(await self.list_prompt_info(readonly_context))

    @property
    def server_label(self) -> str:
        return _server_identity(self._connection_params)[0]

    @property
    def resource_tool_name(self) -> str:
        return f"load_mcp_resource_{_server_identity(self._connection_params)[1]}"

    @property
    def prompt_tool_name(self) -> str:
        return f"load_mcp_prompt_{_server_identity(self._connection_params)[1]}"


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


def _server_identity(connection_params: Any) -> tuple[str, str]:
    parsed = urlparse(str(getattr(connection_params, "url", "")))
    path = parsed.path.rstrip("/")
    headers = getattr(connection_params, "headers", None) or {}
    proxy_host = headers.get(PROXY_HOST_HEADER)
    if proxy_host:
        label = f"{proxy_host}{path}" if path else str(proxy_host)
    else:
        host = parsed.netloc
        if host and path:
            label = f"{host}{path}"
        else:
            label = host or path or "mcp_server"

    slug_source = re.sub(r"[^a-zA-Z0-9]+", "_", label).strip("_").lower() or "mcp_server"
    digest = hashlib.sha1(label.encode("utf-8")).hexdigest()[:8]
    return label, f"{slug_source[:32]}_{digest}"


def _model_dump(value: Any) -> dict[str, Any]:
    if hasattr(value, "model_dump"):
        return value.model_dump(mode="json", exclude_none=True)
    if isinstance(value, dict):
        return value
    try:
        return dict(value)
    except (TypeError, ValueError):
        logger.warning("Cannot convert %s to dict, returning string representation", type(value).__name__)
        return {"raw": str(value)}
