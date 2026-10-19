"""Optional filesystem and bash tools for BYO OpenAI agents."""

from __future__ import annotations

from pathlib import Path

from agents.exceptions import UserError
from agents.run_context import RunContextWrapper
from agents.tool import function_tool
from kagent.tools import (
    edit_file_content,
    execute_command,
    get_bash_description,
    get_edit_file_description,
    get_read_file_description,
    get_workspace_path,
    get_write_file_description,
    read_file_content,
    write_file_content,
)

from .._agent_executor import SessionContext

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
        working_dir = get_workspace_path()
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path

        return read_file_content(path, offset, limit)
    except (FileNotFoundError, IsADirectoryError, PermissionError, OSError) as e:
        raise UserError(str(e)) from e


@function_tool(
    name_override="write_file",
    description_override=get_write_file_description(),
)
def write_file(wrapper: RunContextWrapper[SessionContext], file_path: str, content: str) -> str:
    """Write content to a file."""
    try:
        working_dir = get_workspace_path()
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
        working_dir = get_workspace_path()
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
async def bash(wrapper: RunContextWrapper[SessionContext], command: str, timeout_seconds: int = 0) -> str:
    """Execute a bash command in the agent sandbox."""
    try:
        return await execute_command(command, get_workspace_path(), timeout_seconds=timeout_seconds)
    except Exception as e:
        raise UserError(f"Error executing command: {e}") from e
