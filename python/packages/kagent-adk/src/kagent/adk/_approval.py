"""before_tool_callback implementation for HITL tool approval."""

import hashlib
import json
import logging
from typing import Any, Optional

from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext

logger = logging.getLogger(__name__)

# Marker value returned by the callback to signal approval is required.
# The executor inspects tool response events for this marker.
APPROVAL_REQUIRED_STATUS = "__KAGENT_APPROVAL_REQUIRED__"

# Session state key for storing pending tool calls awaiting approval
HITL_PENDING_TOOLS_KEY = "_hitl_pending_tools"


def make_approval_key(tool_name: str, tool_args: dict[str, Any]) -> str:
    """Create a session state key for tracking tool approval.

    Uses tool name and a stable hash of arguments to create a unique key.
    This ensures that approving delete_file(path="/tmp/a") doesn't also
    approve delete_file(path="/tmp/b").
    """
    args_str = json.dumps(tool_args, sort_keys=True, default=str)
    args_hash = hashlib.sha256(args_str.encode()).hexdigest()[:12]
    return f"_hitl_approved:{tool_name}:{args_hash}"


def make_approval_callback(
    tools_requiring_approval: set[str],
):
    """Create a before_tool_callback that blocks tools requiring approval.

    Args:
        tools_requiring_approval: Set of tool names that need human approval.

    Returns:
        A callback compatible with Google ADK's before_tool_callback signature.
    """

    def before_tool(
        tool: BaseTool,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        tool_name = tool.name
        if tool_name not in tools_requiring_approval:
            return None  # No approval needed, proceed normally

        # Check session state for a prior approval for this tool call
        approval_key = make_approval_key(tool_name, args)
        if tool_context.state.get(approval_key):
            # Approval found — consume it (one-time use) and proceed.
            # Use state_delta to clear the key; ToolContext.state doesn't support __delitem__.
            tool_context.state[approval_key] = None
            logger.info("Consumed approval for tool %s (key=%s)", tool_name, approval_key)
            return None

        # Block tool execution, return marker for executor to detect
        logger.info("Tool %s requires approval, blocking execution", tool_name)
        return {
            "status": APPROVAL_REQUIRED_STATUS,
            "tool": tool_name,
            "args": args,
        }

    return before_tool
