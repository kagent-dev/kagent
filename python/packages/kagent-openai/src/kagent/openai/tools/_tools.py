"""File operation and skill tools for agents.

This module provides Read, Write, Edit, Bash, and Skills tools that agents can use.
These tools are wrappers around the centralized logic in the kagent-skills package.
"""

from __future__ import annotations

import logging
from pathlib import Path

from agents.exceptions import UserError
from agents.run_context import RunContextWrapper
from agents.tool import FunctionTool, function_tool

from kagent.skills import (
    discover_skills,
    edit_file_content,
    execute_command,
    generate_skills_tool_description,
    get_bash_description,
    get_edit_file_description,
    get_read_file_description,
    get_session_path,
    get_write_file_description,
    initialize_session_path,
    load_skill_content,
    read_file_content,
    write_file_content,
)

from .._agent_executor import SessionContext

logger = logging.getLogger(__name__)


# --- System Tools ---


@function_tool(
    name_override="read_file",
    description_override=get_read_file_description(),
)
def read_file(
    wrapper: RunContextWrapper[SessionContext],
    file_path: str,
    offset: int | None = None,
    limit: int | None = None,
) -> str:
    """Read a file from the filesystem."""
    try:
        session_id = wrapper.context.session_id
        working_dir = get_session_path(session_id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path

        return read_file_content(path, offset, limit)
    except (FileNotFoundError, IsADirectoryError, OSError) as e:
        raise UserError(str(e)) from e


@function_tool(
    name_override="write_file",
    description_override=get_write_file_description(),
)
def write_file(wrapper: RunContextWrapper[SessionContext], file_path: str, content: str) -> str:
    """Write content to a file."""
    try:
        session_id = wrapper.context.session_id
        working_dir = get_session_path(session_id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path

        return write_file_content(path, content)
    except OSError as e:
        raise UserError(str(e)) from e


@function_tool(
    name_override="edit_file",
    description_override=get_edit_file_description(),
)
def edit_file(
    wrapper: RunContextWrapper[SessionContext],
    file_path: str,
    old_string: str,
    new_string: str,
    replace_all: bool = False,
) -> str:
    """Edit a file by replacing old_string with new_string."""
    try:
        session_id = wrapper.context.session_id
        working_dir = get_session_path(session_id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path

        return edit_file_content(path, old_string, new_string, replace_all)
    except (FileNotFoundError, IsADirectoryError, ValueError, OSError) as e:
        raise UserError(str(e)) from e


@function_tool(
    name_override="bash",
    description_override=get_bash_description(),
)
async def bash(wrapper: RunContextWrapper[SessionContext], command: str) -> str:
    """Executes a bash command in a sandboxed environment."""
    try:
        session_id = wrapper.context.session_id
        working_dir = get_session_path(session_id)
        return await execute_command(command, working_dir)
    except Exception as e:
        raise UserError(f"Error executing command: {e}") from e


# --- Skill Tools ---


def get_skill_tool(skills_directory: str | Path = "/skills") -> FunctionTool:
    """Create a Skill tool.

    This function generates a tool instance with skills discovered from the provided
    directory, following the ADK pattern.
    """
    skills_dir = Path(skills_directory)
    if not skills_dir.exists():
        raise ValueError(f"Skills directory does not exist: {skills_dir}")

    # Discover skills and generate the tool description.
    skills = discover_skills(skills_dir)
    description = generate_skills_tool_description(skills)

    @function_tool(name_override="skills", description_override=description)
    def skill_tool_impl(wrapper: RunContextWrapper[SessionContext], command: str) -> str:
        """Execute a skill by name.

        Args:
            command: The name of the skill to execute (e.g., "data-analysis")

        Returns:
            The full skill instructions and context.
        """
        # This function is cached internally, so calling it multiple times is safe.
        initialize_session_path(wrapper.context.session_id, str(skills_dir))
        skill_name = command.strip()

        try:
            content = load_skill_content(skills_dir, skill_name)

            # Mimic ADK's formatting
            header = (
                f'<command-message>The "{skill_name}" skill is loading</command-message>\n\n'
                f"Base directory for this skill: {skills_dir.resolve()}/{skill_name}\n\n"
            )
            footer = (
                "\n\n---\n"
                "The skill has been loaded. Follow the instructions above and use the bash tool to execute commands."
            )
            return header + content + footer

        except (FileNotFoundError, OSError) as e:
            return f"Error loading skill '{skill_name}': {e}"
        except Exception as e:
            return f"An unexpected error occurred while loading skill '{skill_name}': {e}"

    return skill_tool_impl


def get_skill_tools(skills_directory: str | Path = "/skills") -> list[FunctionTool]:
    """
    Create a list of tools including the skill tool and file operation tools.

    Args:
        skills_directory: Path to the directory containing skills.

    Returns:
        A list of FunctionTool instances: skills tool, read_file, write_file, edit_file
    """
    return [get_skill_tool(skills_directory), read_file, write_file, edit_file, bash]
