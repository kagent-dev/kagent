"""Simplified bash tool for executing shell commands in skills context."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

from kagent.skills import execute_command, get_bash_description, get_session_path

logger = logging.getLogger("kagent_adk." + __name__)


class BashTool(BaseTool):
    """Execute bash commands safely in the skills environment.

    This tool uses the Anthropic Sandbox Runtime (srt) to execute commands with:
    - Filesystem restrictions (controlled read/write access)
    - Network restrictions (controlled domain access)
    - Process isolation at the OS level

    Use it for command-line operations like running scripts, installing packages, etc.
    For file operations (read/write/edit), use the dedicated file tools instead.
    """

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="bash",
            description=get_bash_description(),
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
                    "command": types.Schema(
                        type=types.Type.STRING,
                        description="Bash command to execute. Use && to chain commands.",
                    ),
                    "description": types.Schema(
                        type=types.Type.STRING,
                        description="Clear, concise description of what this command does (5-10 words)",
                    ),
                },
                required=["command"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Execute a bash command safely using the Anthropic Sandbox Runtime."""
        command = args.get("command", "").strip()
        description = args.get("description", "")

        if not command:
            return "Error: No command provided"

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)
            result = await execute_command(command, working_dir)
            logger.info(f"Executed bash command: {command}, description: {description}")
            return result
        except Exception as e:
            error_msg = f"Error executing command '{command}': {e}"
            logger.error(error_msg)
            return error_msg
