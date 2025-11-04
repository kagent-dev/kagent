from __future__ import annotations

import logging
from pathlib import Path
from typing import Optional

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.agents.callback_context import CallbackContext
from google.adk.plugins import BasePlugin
from google.genai import types

from ..artifacts import initialize_session_path
from ..tools import BashTool, EditFileTool, ReadFileTool, WriteFileTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


class SkillsPlugin(BasePlugin):
    """Convenience plugin for multi-agent apps to automatically register Skills tools.

    This plugin is purely a convenience wrapper that automatically adds the SkillsTool
    and BashTool and related file tools to all LLM agents in an application.
    It does not add any additional functionality beyond tool registration.

    For single-agent use cases or when you prefer explicit control, you can skip this plugin
    and directly add both tools to your agent's tools list.

    Example:
        # Without plugin (direct tool usage):
        agent = Agent(
            tools=[
                SkillsTool(skills_directory="./skills"),
                BashTool(skills_directory="./skills"),
                ReadFileTool(),
                WriteFileTool(),
                EditFileTool(),
            ]
        )

        # With plugin (auto-registration for multi-agent apps):
        app = App(
            root_agent=agent,
            plugins=[SkillsPlugin(skills_directory="./skills")]
        )
    """

    def __init__(self, skills_directory: str | Path, name: str = "skills_plugin"):
        """Initialize the skills plugin.

        Args:
          skills_directory: Path to directory containing skill folders.
          name: Name of the plugin instance.
        """
        super().__init__(name)
        self.skills_directory = Path(skills_directory)

    async def before_agent_callback(
        self, *, agent: BaseAgent, callback_context: CallbackContext
    ) -> Optional[types.Content]:
        """Initialize session path and add skills tools to agents if not already present.

        This hook fires before any tools are invoked, ensuring the session working
        directory is set up with the skills symlink before any tool needs it.
        """
        # Initialize session path FIRST (before tools run)
        # This creates the working directory structure and skills symlink
        session_id = callback_context.session.id
        initialize_session_path(session_id, str(self.skills_directory))
        logger.debug(f"Initialized session path for session: {session_id}")

        add_skills_tool_to_agent(self.skills_directory, agent)


def add_skills_tool_to_agent(skills_directory: str | Path, agent: BaseAgent) -> None:
    """Utility function to add Skills and Bash tools to a given agent.

    Args:
      agent: The LlmAgent instance to which the tools will be added.
      skills_directory: Path to directory containing skill folders.
    """

    if not isinstance(agent, LlmAgent):
        return

    skills_directory = Path(skills_directory)
    existing_tool_names = {getattr(t, "name", None) for t in agent.tools}

    # Add SkillsTool if not already present
    if "skills" not in existing_tool_names:
        agent.tools.append(SkillsTool(skills_directory))
        logger.debug(f"Added skills invoke tool to agent: {agent.name}")

    # Add BashTool if not already present
    if "bash" not in existing_tool_names:
        agent.tools.append(BashTool(skills_directory))
        logger.debug(f"Added bash tool to agent: {agent.name}")

    if "read_file" not in existing_tool_names:
        agent.tools.append(ReadFileTool())
        logger.debug(f"Added read file tool to agent: {agent.name}")

    if "write_file" not in existing_tool_names:
        agent.tools.append(WriteFileTool())
        logger.debug(f"Added write file tool to agent: {agent.name}")

    if "edit_file" not in existing_tool_names:
        agent.tools.append(EditFileTool())
        logger.debug(f"Added edit file tool to agent: {agent.name}")
