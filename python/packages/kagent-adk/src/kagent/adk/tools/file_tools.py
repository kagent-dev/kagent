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
    get_grep_file_description,
    get_list_files_description,
    get_read_file_description,
    get_session_path,
    get_write_file_description,
    grep_content,
    list_dir_content,
    read_file_content,
    write_file_content,
)

logger = logging.getLogger("kagent_adk." + __name__)


class ReadFileTool(BaseTool):
    """Read files with line numbers for precise editing."""

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="read_file",
            description=get_read_file_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

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

            return read_file_content(path, offset, limit, allowed_root=[working_dir, Path(self.skills_directory)])
        except (FileNotFoundError, IsADirectoryError, PermissionError, IOError) as e:
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

            return write_file_content(path, content, allowed_root=working_dir)
        except (PermissionError, IOError) as e:
            error_msg = f"Error writing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg


class ListFilesTool(BaseTool):
    """List files and directories at a given path."""

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="list_files",
            description=get_list_files_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "path": types.Schema(
                        type=types.Type.STRING,
                        description="Directory path to list (absolute or relative to working directory); defaults to the working directory",
                    ),
                },
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """List the contents of a directory."""
        path_str = args.get("path", "").strip() or "."

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            path = Path(path_str)
            if not path.is_absolute():
                path = working_dir / path
            path = path.resolve()

            return list_dir_content(path, allowed_root=[working_dir, Path(self.skills_directory)])
        except (FileNotFoundError, NotADirectoryError, PermissionError, IOError) as e:
            return f"Error listing {path_str}: {e}"


class GrepFileTool(BaseTool):
    """Search for a regular expression pattern in a file or directory."""

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="grep_file",
            description=get_grep_file_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "pattern": types.Schema(
                        type=types.Type.STRING,
                        description="The regular expression pattern to search for",
                    ),
                    "path": types.Schema(
                        type=types.Type.STRING,
                        description="The file or directory path to search (absolute or relative to working directory)",
                    ),
                    "recursive": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Search directories recursively (default: false)",
                    ),
                    "ignore_case": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Ignore case when matching (default: false)",
                    ),
                },
                required=["pattern", "path"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Search a file or directory for a pattern."""
        pattern = args.get("pattern", "").strip()
        path_str = args.get("path", "").strip()
        recursive = args.get("recursive", False)
        ignore_case = args.get("ignore_case", False)

        if not pattern:
            return "Error: No pattern provided"
        if not path_str:
            return "Error: No file path provided"

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            path = Path(path_str)
            if not path.is_absolute():
                path = working_dir / path
            path = path.resolve()

            return grep_content(
                path,
                pattern,
                recursive=recursive,
                ignore_case=ignore_case,
                allowed_root=[working_dir, Path(self.skills_directory)],
            )
        except (FileNotFoundError, IsADirectoryError, ValueError, PermissionError, IOError) as e:
            return f"Error searching {path_str}: {e}"


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

            return edit_file_content(path, old_string, new_string, replace_all, allowed_root=working_dir)
        except (FileNotFoundError, IsADirectoryError, ValueError, PermissionError, IOError) as e:
            error_msg = f"Error editing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg
