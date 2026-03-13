from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

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

    @override
    async def process_llm_request(
        self,
        *,
        tool_context: ToolContext,
        llm_request: LlmRequest,
    ) -> None:
        session = tool_context.session
        if not session:
            return
        info = (
            "kagent session:\n"
            f"- session_id: {session.id or 'N/A'}\n"
            f"- user_id: {session.user_id or 'N/A'}\n"
            f"- app_name: {session.app_name or 'N/A'}"
        )
        llm_request.append_instructions([info])
