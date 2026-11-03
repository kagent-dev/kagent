"""Bash tool for a Substrate Actor."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from kagent.tools import execute_command, get_bash_description, get_workspace_path

logger = logging.getLogger("kagent_adk." + __name__)


class BashTool(BaseTool):
    """Execute bash commands in the agent runtime."""

    def __init__(
        self, skills_directory: str | Path | None, *, file_search_tools: bool = True, bash_venv_path: str = ""
    ):
        super().__init__(
            name="bash",
            description=get_bash_description(file_search_tools)
            + (
                f"\nSkills directory: {Path(skills_directory).resolve()}. "
                "Execute skill scripts using their absolute paths."
                if skills_directory
                else ""
            ),
        )
        self.bash_venv_path = bash_venv_path
        self.skills_directory = Path(skills_directory).resolve() if skills_directory else None

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
                    "timeout_seconds": types.Schema(
                        type=types.Type.INTEGER,
                        description="Timeout in seconds, 1 to 600; omitted or 0 uses 60 seconds.",
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
        """Execute a bash command."""
        command = args.get("command", "").strip()

        if not command:
            return "Error: No command provided"

        try:
            working_dir = get_workspace_path()
            result = await execute_command(
                command,
                working_dir,
                self.skills_directory,
                bash_venv_path=self.bash_venv_path,
                timeout_seconds=args.get("timeout_seconds", 0),
            )
            return result
        except Exception as e:
            error_msg = f"Error executing command: {e}"
            logger.error(error_msg)
            return error_msg
