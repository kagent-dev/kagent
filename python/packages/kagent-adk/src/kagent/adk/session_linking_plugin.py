from __future__ import annotations

import logging
from typing import Any

from google.adk.plugins.base_plugin import BasePlugin
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext

from kagent.core.a2a import (
    KAGENT_METADATA_KEY_CALLER_SESSION_ID,
    KAGENT_METADATA_KEY_CALLER_TOOL_CALL_ID,
    get_kagent_metadata_key,
)

logger = logging.getLogger("kagent_adk." + __name__)


class SessionLinkingPlugin(BasePlugin):
    """A plugin that propagates parent session metadata to child agents.

    This plugin intercepts tool calls to AgentTool and injects the parent's
    session_id and tool_call_id into the session state.
    Because AgentTool clones the current session state when creating a child
    session, these values are automatically propagated to the sub-agent and
    eventually to the remote A2A task metadata.
    """

    def __init__(self, name: str = "session_linking"):
        super().__init__(name)

    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> None:
        """Inject parent metadata into the state before an AgentTool runs."""
        # We only need to do this for tools that spin up sub-sessions (AgentTools)
        if isinstance(tool, AgentTool):
            invocation_context = tool_context._invocation_context
            if not invocation_context:
                logger.warning(
                    "No invocation context found in tool_context for tool %s. Cannot link sessions.",
                    tool.name,
                )
                return

            # Parent metadata keys. We use a prefix that is NOT filtered out by
            # AgentTool (which typically filters '_adk').
            parent_metadata = {
                get_kagent_metadata_key(KAGENT_METADATA_KEY_CALLER_SESSION_ID): invocation_context.session.id,
                get_kagent_metadata_key(KAGENT_METADATA_KEY_CALLER_TOOL_CALL_ID): tool_context.function_call_id,
            }

            logger.info(
                "Injecting parent metadata into session state for AgentTool %s: %s",
                tool.name,
                parent_metadata,
            )

            # Update the current session state. This state is then cloned by
            # the AgentTool.run_async method to create the child session.
            tool_context.state.update(parent_metadata)
