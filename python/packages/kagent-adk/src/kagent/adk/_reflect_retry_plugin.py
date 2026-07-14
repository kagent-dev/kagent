from typing import Any, Optional

from google.adk.plugins import ReflectAndRetryToolPlugin
from google.adk.tools import BaseTool, ToolContext


class KAgentReflectAndRetryToolPlugin(ReflectAndRetryToolPlugin):
    """ReflectAndRetryToolPlugin that handles both tool failure modes.

    Two ways a tool call can fail:

    1. The tool raises an exception — handled by the base class via
       ``on_tool_error_callback`` (inherited unchanged).
    2. The tool returns normally but reports an error in-band — MCP tools
       never raise; they return a CallToolResult dict with ``isError: True``.
       The base plugin ignores these, so we override
       ``extract_error_from_result`` to treat them as failures.

    Both paths feed the same per-tool failure counter and reflect-and-retry
    flow, and a successful call resets the counter.
    """

    async def extract_error_from_result(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
        result: Any,
    ) -> Optional[Any]:
        if isinstance(result, dict) and result.get("isError"):
            return result
        return None
