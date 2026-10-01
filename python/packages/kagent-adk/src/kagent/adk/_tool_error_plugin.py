"""Convert tool failures into responses that an agent can reason about."""

from typing import Any

from google.adk.plugins import BasePlugin
from google.adk.tools import BaseTool, ToolContext


class ToolErrorPlugin(BasePlugin):
    """Return tool errors to the model instead of aborting the turn."""

    def __init__(self):
        super().__init__(name="tool_error")

    async def on_tool_error_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
        error: Exception,
    ) -> dict[str, Any]:
        return {"error": str(error)}
