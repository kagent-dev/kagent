"""File operation tools for agents.

This module provides Read, Write, Edit, and Bash tools that agents can use to work with
files on the filesystem. These tools are modeled after Claude Code's file tools.
"""

from __future__ import annotations

import logging
import subprocess
from pathlib import Path

from agents import FunctionTool, UserError, function_tool

logger = logging.getLogger("kagent.openai.agents.tools")


@function_tool(
    name_override="read_file",
    description_override="""Reads a file from the local filesystem.
You can access any file directly by using this tool.
If the User provides a path to a file assume that path is valid.
It is okay to read a file that does not exist; an error will be returned.

Usage:
- The file_path parameter must be an absolute path, not a relative path
- By default, it reads up to 2000 lines starting from the beginning of the file
- You can optionally specify a line offset and limit (especially handy for long files),
  but it's recommended to read the whole file by not providing these parameters
- Any lines longer than 2000 characters will be truncated
- Lines are numbered starting at 1 using format: LINE_NUMBER|LINE_CONTENT
- This tool can read images (PNG, JPG, etc) when the file path points to an image
- This tool can only read files, not directories
- You can call multiple tools in a single response. It is always better to
  speculatively read multiple potentially useful files in parallel.""",
)
def read_file(
    file_path: str,
    offset: int | None = None,
    limit: int | None = None,
) -> str:
    """Read a file from the filesystem.

    Args:
        file_path: Absolute path to the file to read.
        offset: Optional line number to start reading from (1-indexed).
        limit: Optional number of lines to read.

    Returns:
        File contents with line numbers.

    Raises:
        UserError: If file doesn't exist or cannot be read.
    """
    path = Path(file_path)

    if not path.exists():
        raise UserError(f"File not found: {file_path}")

    if not path.is_file():
        raise UserError(f"Path is not a file: {file_path}\nThis tool can only read files, not directories.")

    try:
        lines = path.read_text().splitlines()
    except Exception as e:
        raise UserError(f"Error reading file {file_path}: {e}") from e

    # Handle offset and limit
    start = (offset - 1) if offset and offset > 0 else 0
    end = (start + limit) if limit else len(lines)

    # Format with line numbers
    result_lines = []
    for i, line in enumerate(lines[start:end], start=start + 1):
        # Truncate long lines
        if len(line) > 2000:
            line = line[:2000] + "..."
        result_lines.append(f"{i:6d}|{line}")

    if not result_lines:
        return "File is empty."

    return "\n".join(result_lines)


@function_tool(
    name_override="write_file",
    description_override="""Writes a file to the local filesystem.

Usage:
- This tool will overwrite the existing file if there is one at the provided path
- If this is an existing file, you MUST use the read_file tool first to
  read the file's contents
- ALWAYS prefer editing existing files in the codebase.
  NEVER write new files unless explicitly required
- NEVER proactively create documentation files (*.md) or README files.
  Only create documentation files if explicitly requested by the User""",
)
def write_file(file_path: str, content: str) -> str:
    """Write content to a file.

    Args:
        file_path: Absolute path to the file to write (must be absolute, not relative).
        content: The content to write to the file.

    Returns:
        Success message.

    Raises:
        UserError: If file cannot be written.
    """
    path = Path(file_path)

    try:
        # Create parent directories if needed
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return f"Successfully wrote to {file_path}"
    except Exception as e:
        raise UserError(f"Error writing file {file_path}: {e}") from e


@function_tool(
    name_override="edit_file",
    description_override="""Performs exact string replacements in files.

Usage:
- You must use your read_file tool at least once in the conversation before editing.
  This tool will error if you attempt an edit without reading the file
- When editing text from read_file tool output, ensure you preserve the exact
  indentation (tabs/spaces) as it appears AFTER the line number prefix.
  The line number prefix format is: spaces + line number + tab.
  Everything after that tab is the actual file content to match.
  Never include any part of the line number prefix in the old_string or new_string
- ALWAYS prefer editing existing files in the codebase.
  NEVER write new files unless explicitly required
- Only use emojis if the user explicitly requests it.
  Avoid adding emojis to files unless asked
- The edit will FAIL if old_string is not unique in the file.
  Either provide a larger string with more surrounding context to make it unique
  or use replace_all to change every instance of old_string
- Use replace_all for replacing and renaming strings across the file.
  This parameter is useful if you want to rename a variable for instance""",
)
def edit_file(
    file_path: str,
    old_string: str,
    new_string: str,
    replace_all: bool = False,
) -> str:
    """Edit a file by replacing old_string with new_string.

    Args:
        file_path: Absolute path to the file to modify.
        old_string: The text to replace.
        new_string: The text to replace it with (must be different from old_string).
        replace_all: Replace all occurrences of old_string (default False).

    Returns:
        Success message with number of replacements made.

    Raises:
        UserError: If file doesn't exist, old_string not found, or not unique
                   (when replace_all=False).
    """
    if old_string == new_string:
        raise UserError("old_string and new_string must be different")

    path = Path(file_path)

    if not path.exists():
        raise UserError(f"File not found: {file_path}")

    if not path.is_file():
        raise UserError(f"Path is not a file: {file_path}")

    try:
        content = path.read_text()
    except Exception as e:
        raise UserError(f"Error reading file {file_path}: {e}") from e

    # Check if old_string exists
    if old_string not in content:
        raise UserError(
            f"old_string not found in {file_path}.\n"
            f"Make sure you've read the file first and are using the exact string."
        )

    # Count occurrences
    count = content.count(old_string)

    if not replace_all and count > 1:
        raise UserError(
            f"old_string appears {count} times in {file_path}.\n"
            f"Either provide more context to make it unique, or set "
            f"replace_all=True to replace all occurrences."
        )

    # Perform replacement
    if replace_all:
        new_content = content.replace(old_string, new_string)
    else:
        new_content = content.replace(old_string, new_string, 1)

    try:
        path.write_text(new_content)
        return f"Successfully replaced {count} occurrence(s) in {file_path}"
    except Exception as e:
        raise UserError(f"Error writing file {file_path}: {e}") from e


@function_tool(
    name_override="str_shell",
    description_override="""Executes a bash command with optional timeout.

IMPORTANT: This tool is for terminal operations like git, npm, docker, ls, grep, find, echo, cat, head, tail, sed, awk, etc.

Usage:
- Always quote file paths that contain spaces with double quotes
  (e.g., cd "path with spaces/file.txt")
- Use '&&' to chain commands that depend on each other
  (e.g., "git add . && git commit -m 'message'")
- Use ';' only when you need to run commands sequentially but don't care if earlier
  commands fail
- DO NOT use newlines to separate commands (newlines are ok in quoted strings)
- Try to maintain your current working directory throughout the session by using
  absolute paths and avoiding usage of cd

writing files:
- This tool will overwrite the existing file if there is one at the provided path
- If this is an existing file, you MUST use the read_file tool first to
  read the file's contents
- ALWAYS prefer editing existing files in the codebase.
  NEVER write new files unless explicitly required
- NEVER proactively create documentation files (*.md) or README files.
  Only create documentation files if explicitly requested by the User

reading files:
- must be an absolute path, not a relative path
- By default, it read up to 2000 lines starting from the beginning of the file
- You can optionally specify a line offset and limit (especially handy for long files),
  but it's recommended to read the whole file by not providing these parameters
- Any lines longer than 2000 characters will be truncated
- Lines should be numbered starting at 1 using format: LINE_NUMBER|LINE_CONTENT
- This tool can read images (PNG, JPG, etc) when the file path points to an image
- This tool can only read files, not directories
- You can call multiple tools in a single response. It is always better to
  speculatively read multiple potentially useful files in parallel.

editing files:
- You must use this tool to read the file first before editing it.
- When editing text from this tool's output, ensure you preserve the exact
  indentation (tabs/spaces) as it appears AFTER the line number prefix.
  The line number prefix format is: spaces + line number + tab.
  Everything after that tab is the actual file content to match.
  Never include any part of the line number prefix in the old_string or new_string
- ALWAYS prefer editing existing files in the codebase.
  NEVER write new files unless explicitly required
- Only use emojis if the user explicitly requests it.
  Avoid adding emojis to files unless asked
- The edit will FAIL if old_string is not unique in the file.
  Either provide a larger string with more surrounding context to make it unique
  or use replace_all to change every instance of old_string
- Use replace_all for replacing and renaming strings across the file.
  This parameter is useful if you want to rename a variable for instance
""",
)
def srt_shell(
    command: str,
    timeout: int = 120000,
) -> str:
    """Execute a shell command with an srt sandbox.

    srt sandbox is a sandboxed environment that allows you to execute shell commands with a limited set of allowed commands.
    https://github.com/anthropic-experimental/sandbox-runtime

    Args:
        command: The shell command to execute.
        timeout: Optional timeout in milliseconds (default 120000ms = 2 minutes).

    Returns:
        The stdout and stderr output of the command.

    Raises:
        UserError: If command fails or times out.
    """
    try:
        timeout_seconds = timeout / 1000.0
        command = f'srt "{command}"'
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )

        output = result.stdout
        if result.stderr:
            output += "\n" + result.stderr

        if result.returncode != 0:
            return f"Exit code: {result.returncode}\n\n{output}"

        return f"Exit code: 0\n\n{output}"

    except subprocess.TimeoutExpired:
        raise UserError(
            f"Command timed out after {timeout}ms. Consider breaking it into smaller steps or increasing the timeout."
        )
    except Exception as e:
        raise UserError(f"Error executing command: {e}") from e


# Export tool instances
READ_FILE_TOOL: FunctionTool = read_file
WRITE_FILE_TOOL: FunctionTool = write_file
EDIT_FILE_TOOL: FunctionTool = edit_file
SRT_SHELL_TOOL: FunctionTool = srt_shell
