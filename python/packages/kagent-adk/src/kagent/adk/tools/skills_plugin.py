from __future__ import annotations

import logging
from pathlib import Path
from typing import Optional

from google.adk.agents import BaseAgent, LlmAgent
from kagent.skills import file_search_tools_enabled

from ..tools import BashTool, EditFileTool, GrepFileTool, ListFilesTool, ReadFileTool, WriteFileTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


def add_skills_tool_to_agent(
    skills_directory: str | Path,
    agent: BaseAgent,
) -> None:
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
        agent.tools.append(ReadFileTool(skills_directory))
        logger.debug(f"Added read file tool to agent: {agent.name}")

    if "write_file" not in existing_tool_names:
        agent.tools.append(WriteFileTool())
        logger.debug(f"Added write file tool to agent: {agent.name}")

    if "edit_file" not in existing_tool_names:
        agent.tools.append(EditFileTool())
        logger.debug(f"Added edit file tool to agent: {agent.name}")

    # list_files/grep_file are opt-in: they give an agent broad filesystem
    # visibility, so some deployments want them off unless explicitly
    # enabled, same as bash.
    if file_search_tools_enabled():
        if "list_files" not in existing_tool_names:
            agent.tools.append(ListFilesTool(skills_directory))
            logger.debug(f"Added list files tool to agent: {agent.name}")

        if "grep_file" not in existing_tool_names:
            agent.tools.append(GrepFileTool(skills_directory))
            logger.debug(f"Added grep file tool to agent: {agent.name}")
    else:
        logger.debug(f"Omitting list_files/grep_file tools for agent: {agent.name} (KAGENT_ENABLE_FILE_SEARCH_TOOLS not enabled)")
