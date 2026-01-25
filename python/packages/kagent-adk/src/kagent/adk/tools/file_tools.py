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

from kagent.skills import (
    edit_file_content,
    get_edit_file_description,
    get_read_file_description,
    get_session_path,
    get_write_file_description,
    read_file_content,
    write_file_content,
)

logger = logging.getLogger("kagent_adk." + __name__)


class ReadFileTool(BaseTool):
    """Read files with line numbers for precise editing."""

    def __init__(self):
        super().__init__(
            name="read_file",
            description=get_read_file_description(),
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
        file_path_str = args.get("file_path", "").strip()
        offset = args.get("offset")
        limit = args.get("limit")

        if not file_path_str:
            return "Error: No file path provided"

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            path = Path(file_path_str)
            if not path.is_absolute():
                path = working_dir / path
            path = path.resolve()

            return read_file_content(path, offset, limit)
        except (FileNotFoundError, IsADirectoryError, IOError) as e:
            return f"Error reading file {file_path_str}: {e}"


class WriteFileTool(BaseTool):
    """Write content to files (overwrites existing files)."""

    def __init__(self):
        super().__init__(
            name="write_file",
            description=get_write_file_description(),
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
        file_path_str = args.get("file_path", "").strip()
        content = args.get("content", "")

        if not file_path_str:
            return "Error: No file path provided"

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            path = Path(file_path_str)
            if not path.is_absolute():
                path = working_dir / path
            path = path.resolve()

            return write_file_content(path, content)
        except IOError as e:
            error_msg = f"Error writing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg


class EditFileTool(BaseTool):
    """Edit files by replacing exact string matches."""

    def __init__(self):
        super().__init__(
            name="edit_file",
            description=get_edit_file_description(),
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
        file_path_str = args.get("file_path", "").strip()
        old_string = args.get("old_string", "")
        new_string = args.get("new_string", "")
        replace_all = args.get("replace_all", False)

        if not file_path_str:
            return "Error: No file path provided"

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            path = Path(file_path_str)
            if not path.is_absolute():
                path = working_dir / path
            path = path.resolve()

            return edit_file_content(path, old_string, new_string, replace_all)
        except (FileNotFoundError, IsADirectoryError, ValueError, IOError) as e:
            error_msg = f"Error editing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg
