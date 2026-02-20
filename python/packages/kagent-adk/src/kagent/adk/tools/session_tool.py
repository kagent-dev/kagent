from __future__ import annotations

import json
import logging
from typing import Any, Dict

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.tools import BaseTool, ToolContext
from google.genai import types

logger = logging.getLogger("kagent_adk." + __name__)


def add_session_tool(agent: BaseAgent) -> None:
    if not isinstance(agent, LlmAgent):
        return
    existing_tool_names = {getattr(t, "name", None) for t in agent.tools}
    if "get_session_info" not in existing_tool_names:
        agent.tools.append(SessionInfoTool())
        logger.debug(f"Added session info tool to agent: {agent.name}")


class SessionInfoTool(BaseTool):
    """Tool for retrieving information about the current agent session."""

    def __init__(self):
        super().__init__(
            name="get_session_info",
            description="Get information about the current agent session, including the session ID.",
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={},
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        info = {
            "session_id": tool_context.session.id,
            "user_id": tool_context.session.user_id,
            "app_name": tool_context.session.app_name,
        }
        return json.dumps(info)
