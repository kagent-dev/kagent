from __future__ import annotations

import logging
from pathlib import Path

from google.adk.agents import BaseAgent, LlmAgent
from kagent.skills import discover_skills

from .bash_tool import BashTool
from .file_tools import EditFileTool, GrepFileTool, ListFilesTool, ReadFileTool, WriteFileTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


def add_builtin_tools_to_agent(
    skills_directory: str | Path,
    agent: BaseAgent,
    *,
    disable_file_search: bool = False,
    bash_venv_path: str = "",
    disable_bash: bool = False,
) -> None:
    """Add built-in tools and any discovered local skills to an LLM agent."""

    if not isinstance(agent, LlmAgent):
        return

    skills_directory = Path(skills_directory)
    existing_tool_names = {getattr(t, "name", None) for t in agent.tools}

    # Add SkillsTool if not already present
    if skills_directory.is_dir() and discover_skills(skills_directory) and "skills" not in existing_tool_names:
        agent.tools.append(SkillsTool(skills_directory))
        logger.debug(f"Added skills invoke tool to agent: {agent.name}")

    # Add BashTool if not already present
    if not disable_bash and "bash" not in existing_tool_names:
        agent.tools.append(
            BashTool(skills_directory, file_search_tools=not disable_file_search, bash_venv_path=bash_venv_path)
        )
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

    # File search tools are available unless explicitly disabled.
    if not disable_file_search:
        if "list_files" not in existing_tool_names:
            agent.tools.append(ListFilesTool(skills_directory))
            logger.debug(f"Added list files tool to agent: {agent.name}")

        if "grep_file" not in existing_tool_names:
            agent.tools.append(GrepFileTool(skills_directory))
            logger.debug(f"Added grep file tool to agent: {agent.name}")
    else:
        logger.debug(
            f"Omitting list_files/grep_file tools for agent: {agent.name} (runtime.builtin_tools.disable_file_search enabled)"
        )
