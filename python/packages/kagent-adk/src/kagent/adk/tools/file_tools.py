"""File operation tools for agent skills.

This module provides Read, Write, and Edit tools that agents can use to work with
files on the filesystem within the sandbox environment.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

from ..artifacts import get_session_path

logger = logging.getLogger("kagent_adk." + __name__)


class ReadFileTool(BaseTool):
    """Read files with line numbers for precise editing."""

    def __init__(self):
        super().__init__(
            name="read_file",
            description=(
                "Reads a file from the filesystem with line numbers.\n\n"
                "Usage:\n"
                "- Provide a path to the file (absolute or relative to your working directory)\n"
                "- Returns content with line numbers (format: LINE_NUMBER|CONTENT)\n"
                "- Optional offset and limit parameters for reading specific line ranges\n"
                "- Lines longer than 2000 characters are truncated\n"
                "- Always read a file before editing it\n"
                "- You can read from skills/ directory, uploads/, outputs/, or any file in your session\n"
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to read (absolute or relative to working directory)",
                    ),
                    "offset": types.Schema(
                        type=types.Type.INTEGER,
                        description="Optional line number to start reading from (1-indexed)",
                    ),
                    "limit": types.Schema(
                        type=types.Type.INTEGER,
                        description="Optional number of lines to read",
                    ),
                },
                required=["file_path"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Read a file with line numbers."""
        file_path = args.get("file_path", "").strip()
        offset = args.get("offset")
        limit = args.get("limit")

        if not file_path:
            return "Error: No file path provided"

        # Resolve path relative to session working directory
        working_dir = get_session_path(session_id=tool_context.session.id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path
        path = path.resolve()

        if not path.exists():
            return f"Error: File not found: {file_path}"

        if not path.is_file():
            return f"Error: Path is not a file: {file_path}\nThis tool can only read files, not directories."

        try:
            lines = path.read_text().splitlines()
        except Exception as e:
            return f"Error reading file {file_path}: {e}"

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


class WriteFileTool(BaseTool):
    """Write content to files (overwrites existing files)."""

    def __init__(self):
        super().__init__(
            name="write_file",
            description=(
                "Writes content to a file on the filesystem.\n\n"
                "Usage:\n"
                "- Provide a path (absolute or relative to working directory) and content to write\n"
                "- Overwrites existing files\n"
                "- Creates parent directories if needed\n"
                "- For existing files, read them first using read_file\n"
                "- Prefer editing existing files over writing new ones\n"
                "- You can write to your working directory, outputs/, or any writable location\n"
                "- Note: skills/ directory is read-only\n"
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to write (absolute or relative to working directory)",
                    ),
                    "content": types.Schema(
                        type=types.Type.STRING,
                        description="Content to write to the file",
                    ),
                },
                required=["file_path", "content"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Write content to a file."""
        file_path = args.get("file_path", "").strip()
        content = args.get("content", "")

        if not file_path:
            return "Error: No file path provided"

        # Resolve path relative to session working directory
        working_dir = get_session_path(session_id=tool_context.session.id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path
        path = path.resolve()

        try:
            # Create parent directories if needed
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
            logger.info(f"Successfully wrote to {file_path}")
            return f"Successfully wrote to {file_path}"
        except Exception as e:
            error_msg = f"Error writing file {file_path}: {e}"
            logger.error(error_msg)
            return error_msg


class EditFileTool(BaseTool):
    """Edit files by replacing exact string matches."""

    def __init__(self):
        super().__init__(
            name="edit_file",
            description=(
                "Performs exact string replacements in files.\n\n"
                "Usage:\n"
                "- You must read the file first using read_file\n"
                "- Provide path (absolute or relative to working directory)\n"
                "- When editing, preserve exact indentation from the file content\n"
                "- Do NOT include line number prefixes in old_string or new_string\n"
                "- old_string must be unique unless replace_all=true\n"
                "- Use replace_all to rename variables/strings throughout the file\n"
                "- old_string and new_string must be different\n"
                "- Note: skills/ directory is read-only\n"
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to edit (absolute or relative to working directory)",
                    ),
                    "old_string": types.Schema(
                        type=types.Type.STRING,
                        description="The exact text to replace (must exist in file)",
                    ),
                    "new_string": types.Schema(
                        type=types.Type.STRING,
                        description="The text to replace it with (must be different from old_string)",
                    ),
                    "replace_all": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Replace all occurrences (default: false, only replaces first occurrence)",
                    ),
                },
                required=["file_path", "old_string", "new_string"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Edit a file by replacing old_string with new_string."""
        file_path = args.get("file_path", "").strip()
        old_string = args.get("old_string", "")
        new_string = args.get("new_string", "")
        replace_all = args.get("replace_all", False)

        if not file_path:
            return "Error: No file path provided"

        if old_string == new_string:
            return "Error: old_string and new_string must be different"

        # Resolve path relative to session working directory
        working_dir = get_session_path(session_id=tool_context.session.id)
        path = Path(file_path)
        if not path.is_absolute():
            path = working_dir / path
        path = path.resolve()

        if not path.exists():
            return f"Error: File not found: {file_path}"

        if not path.is_file():
            return f"Error: Path is not a file: {file_path}"

        try:
            content = path.read_text()
        except Exception as e:
            return f"Error reading file {file_path}: {e}"

        # Check if old_string exists
        if old_string not in content:
            return (
                f"Error: old_string not found in {file_path}.\n"
                f"Make sure you've read the file first and are using the exact string."
            )

        # Count occurrences
        count = content.count(old_string)

        if not replace_all and count > 1:
            return (
                f"Error: old_string appears {count} times in {file_path}.\n"
                f"Either provide more context to make it unique, or set "
                f"replace_all=true to replace all occurrences."
            )

        # Perform replacement
        if replace_all:
            new_content = content.replace(old_string, new_string)
        else:
            new_content = content.replace(old_string, new_string, 1)

        try:
            path.write_text(new_content)
            logger.info(f"Successfully replaced {count} occurrence(s) in {file_path}")
            return f"Successfully replaced {count} occurrence(s) in {file_path}"
        except Exception as e:
            error_msg = f"Error writing file {file_path}: {e}"
            logger.error(error_msg)
            return error_msg
