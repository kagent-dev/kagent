"""Simplified bash tool for executing shell commands in skills context."""

from __future__ import annotations

import asyncio
import logging
import os
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

from ..artifacts import get_session_path

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
            description=(
                "Execute bash commands in the skills environment with sandbox protection.\n\n"
                "Working Directory & Structure:\n"
                "- Commands run in a temporary session directory: /tmp/kagent/{session_id}/\n"
                "- /skills -> All skills are available here (read-only).\n"
                "- Your current working directory is added to PYTHONPATH.\n\n"
                "Python Imports (CRITICAL):\n"
                "- To import from a skill, use the full path from the 'skills' root.\n"
                "  Example: from skills.skills_name.module import function\n\n"
                "- If the skills name contains a dash '-', you need to use importlib to import it.\n"
                "  Example:\n"
                "    import importlib\n"
                "    skill_module = importlib.import_module('skills.skill-name.module')\n\n"
                "For file operations:\n"
                "- Use read_file, write_file, and edit_file for interacting with the filesystem.\n\n"
                "Timeouts:\n"
                "- pip install: 120s\n"
                "- python scripts: 60s\n"
                "- other commands: 30s\n"
            ),
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
            result = await self._execute_command_with_srt(command, tool_context)
            logger.info(f"Executed bash command: {command}, description: {description}")
            return result
        except Exception as e:
            error_msg = f"Error executing command '{command}': {e}"
            logger.error(error_msg)
            return error_msg

    async def _execute_command_with_srt(self, command: str, tool_context: ToolContext) -> str:
        """Execute a bash command safely using the Anthropic Sandbox Runtime.

        The srt (Sandbox Runtime) wraps the command in a secure sandbox that enforces
        filesystem and network restrictions at the OS level.

        The working directory is a temporary session path, which contains:
        - uploads/: staged user files
        - outputs/: location for generated files
        The /skills directory is available at the root and on the PYTHONPATH.
        """
        # Get session working directory (initialized by SkillsPlugin)
        working_dir = get_session_path(session_id=tool_context.session.id)

        # Determine timeout based on command
        timeout = self._get_command_timeout_seconds(command)

        # Prepare environment with PYTHONPATH including skills directory
        # This allows imports like: from skills.slack_gif_creator.core import something
        env = os.environ.copy()
        # Add root for 'from skills...' and working_dir for local scripts
        pythonpath_additions = [str(working_dir), "/"]
        if "PYTHONPATH" in env:
            pythonpath_additions.append(env["PYTHONPATH"])
        env["PYTHONPATH"] = ":".join(pythonpath_additions)

        # Execute with sandbox runtime
        sandboxed_command = f'srt "{command}"'

        try:
            process = await asyncio.create_subprocess_shell(
                sandboxed_command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=working_dir,
                env=env,  # Pass the modified environment
            )

            try:
                stdout, stderr = await asyncio.wait_for(process.communicate(), timeout=timeout)
            except asyncio.TimeoutError:
                process.kill()
                await process.wait()
                return f"Error: Command timed out after {timeout}s"

            stdout_str = stdout.decode("utf-8", errors="replace") if stdout else ""
            stderr_str = stderr.decode("utf-8", errors="replace") if stderr else ""

            # Handle command failure
            if process.returncode != 0:
                error_msg = f"Command failed with exit code {process.returncode}"
                if stderr_str:
                    error_msg += f":\n{stderr_str}"
                elif stdout_str:
                    error_msg += f":\n{stdout_str}"
                return error_msg

            # Return output
            output = stdout_str
            if stderr_str and "WARNING" not in stderr_str:
                output += f"\n{stderr_str}"

            return output.strip() if output.strip() else "Command completed successfully."

        except Exception as e:
            logger.error(f"Error executing command: {e}")
            return f"Error: {e}"

    def _get_command_timeout_seconds(self, command: str) -> float:
        """Determine appropriate timeout for command in seconds.

        Based on the command string, determine the timeout. srt timeout is in milliseconds,
        so we return seconds for asyncio compatibility.
        """
        # Check for keywords in the command to determine timeout
        if "pip install" in command or "pip3 install" in command:
            return 120.0  # 2 minutes for package installations
        elif "python " in command or "python3 " in command:
            return 60.0  # 1 minute for python scripts
        else:
            return 30.0  # 30 seconds for other commands
