"""before_tool_callback implementation for HITL tool approval.

Uses the ADK-native ToolContext.request_confirmation() mechanism.
When a tool in the approval set is invoked:
  - First call: requests confirmation via tool_context, blocks execution.
  - Re-invocation after user responds: checks tool_context.tool_confirmation.
"""

import logging
from typing import Any

from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext

logger = logging.getLogger(__name__)


def make_approval_callback(
    tools_requiring_approval: set[str],
):
    """Create a before_tool_callback that requests confirmation for specified tools.

    Args:
        tools_requiring_approval: Set of tool names that need human approval.

    Returns:
        A callback compatible with Google ADK's before_tool_callback signature.
    """

    def before_tool(
        tool: BaseTool,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> dict | None:
        tool_name = tool.name
        if tool_name not in tools_requiring_approval:
            return None  # No approval needed, proceed normally

        # On re-invocation after confirmation, ADK populates tool_confirmation
        if tool_context.tool_confirmation is not None:
            if tool_context.tool_confirmation.confirmed:
                logger.debug("Tool %s approved by user, proceeding", tool_name)
                return None  # Approved — proceed with tool execution
            logger.debug("Tool %s rejected by user", tool_name)
            return {"status": "rejected", "message": "Tool call was rejected by user."}

        # First invocation — request confirmation and block execution
        logger.debug("Tool %s requires approval, requesting confirmation", tool_name)
        tool_context.request_confirmation(
            hint=f"Tool '{tool_name}' requires approval before execution.",
        )
        return {"status": "confirmation_requested", "tool": tool_name}

    return before_tool
