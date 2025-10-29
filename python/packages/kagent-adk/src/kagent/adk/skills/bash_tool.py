"""Simplified bash tool for executing shell commands in skills context."""

from __future__ import annotations

import asyncio
import logging
import os
import shlex
from pathlib import Path
from typing import Any, Dict, List, Set, Union

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

from .stage_artifacts_tool import get_session_staging_path

logger = logging.getLogger("kagent_adk." + __name__)


class BashTool(BaseTool):
    """Execute bash commands safely in the skills environment.

    This tool is for terminal operations and script execution. Use it after loading
    skill instructions with the skills tool.
    """

    DANGEROUS_COMMANDS: Set[str] = {
        "rm",
        "rmdir",
        "mv",
        "cp",
        "chmod",
        "chown",
        "sudo",
        "su",
        "kill",
        "reboot",
        "shutdown",
        "dd",
        "mount",
        "umount",
        "alias",
        "export",
        "source",
        ".",
        "eval",
        "exec",
    }

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="bash",
            description=(
                "Execute bash commands in the skills environment.\n\n"
                "Use this tool to:\n"
                "- Execute Python scripts from files (e.g., 'python scripts/script.py')\n"
                "- Install dependencies (e.g., 'pip install -r requirements.txt')\n"
                "- Navigate and inspect files (e.g., 'ls', 'cat file.txt')\n"
                "- Run shell commands with relative or absolute paths\n\n"
                "Important:\n"
                "- Always load skill instructions first using the skills tool\n"
                "- Execute scripts from within their skill directory using 'cd skills/SKILL_NAME && ...'\n"
                "- For Python code execution: ALWAYS write code to a file first, then run it with 'python file.py'\n"
                "- Never use 'python -c \"code\"' - write to file first instead\n"
                "- Quote paths with spaces (e.g., 'cd \"path with spaces\"')\n"
                "- pip install commands may take longer (120s timeout)\n"
                "- Python scripts have 60s timeout, other commands 30s\n\n"
                "Security:\n"
                "- Only whitelisted commands allowed (ls, cat, python, pip, etc.)\n"
                "- No destructive operations (rm, mv, chown, etc. blocked)\n"
                "- The sandbox environment provides additional isolation"
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
        """Execute a bash command safely."""
        command = args.get("command", "").strip()
        description = args.get("description", "")

        if not command:
            return "Error: No command provided"

        if description:
            logger.info(f"Executing: {description}")

        try:
            parsed_commands = self._parse_and_validate_command(command)
            result = await self._execute_command_safely(parsed_commands, tool_context)
            logger.info(f"Executed bash command: {command}")
            return result
        except Exception as e:
            error_msg = f"Error executing command '{command}': {e}"
            logger.error(error_msg)
            return error_msg

    def _parse_and_validate_command(self, command: str) -> List[List[str]]:
        """Parse and validate command for security."""
        if "&&" in command:
            parts = [part.strip() for part in command.split("&&")]
        else:
            parts = [command]

        parsed_parts = []
        for part in parts:
            parsed_part = shlex.split(part)
            validation_error = self._validate_command_part(parsed_part)
            if validation_error:
                raise ValueError(validation_error)
            parsed_parts.append(parsed_part)
        return parsed_parts

    def _validate_command_part(self, command_parts: List[str]) -> Union[str, None]:
        """Validate a single command part for security."""
        if not command_parts:
            return "Empty command"

        base_command = command_parts[0]

        if base_command in self.DANGEROUS_COMMANDS:
            return f"Command '{base_command}' is not allowed for security reasons."

        return None

    async def _execute_command_safely(self, parsed_commands: List[List[str]], tool_context: ToolContext) -> str:
        """Execute parsed commands in the sandboxed environment."""
        staging_root = get_session_staging_path(
            session_id=tool_context.session.id,
            app_name=tool_context._invocation_context.app_name,
            skills_directory=self.skills_directory,
        )
        original_cwd = os.getcwd()
        output_parts = []

        try:
            os.chdir(staging_root)

            for i, command_parts in enumerate(parsed_commands):
                if i > 0:
                    output_parts.append(f"\n--- Command {i + 1} ---")

                if command_parts[0] == "cd":
                    if len(command_parts) > 1:
                        target_path = command_parts[1]
                        try:
                            # Resolve the path relative to current directory
                            target_abs = (Path(os.getcwd()) / target_path).resolve()
                            os.chdir(target_abs)
                            current_cwd = os.getcwd()
                            output_parts.append(f"Changed directory to {target_path}")
                            logger.info(f"Changed to {target_path}. Current cwd: {current_cwd}")
                        except (OSError, RuntimeError) as e:
                            output_parts.append(f"Error changing directory: {e}")
                            logger.error(f"Failed to cd to {target_path}: {e}")
                    continue

                # Determine timeout based on command type
                timeout = self._get_command_timeout(command_parts)
                current_cwd = os.getcwd()

                try:
                    process = await asyncio.create_subprocess_exec(
                        *command_parts,
                        stdout=asyncio.subprocess.PIPE,
                        stderr=asyncio.subprocess.PIPE,
                        cwd=current_cwd,
                    )
                    try:
                        stdout, stderr = await asyncio.wait_for(process.communicate(), timeout=timeout)
                    except asyncio.TimeoutError:
                        process.kill()
                        await process.wait()
                        error_msg = f"Command '{' '.join(command_parts)}' timed out after {timeout}s"
                        output_parts.append(f"Error: {error_msg}")
                        logger.error(error_msg)
                        break

                    stdout_str = stdout.decode("utf-8", errors="replace") if stdout else ""
                    stderr_str = stderr.decode("utf-8", errors="replace") if stderr else ""

                    if process.returncode != 0:
                        output = stderr_str or stdout_str
                        error_output = f"Command failed with exit code {process.returncode}:\n{output}"
                        output_parts.append(error_output)
                        # Don't break on pip errors, continue to allow retry
                        if command_parts[0] not in ("pip", "pip3"):
                            break
                    else:
                        # Combine stdout and stderr for complete output
                        combined_output = stdout_str
                        if stderr_str and "WARNING" not in stderr_str:
                            combined_output += f"\n{stderr_str}"
                        output_parts.append(
                            combined_output.strip() if combined_output.strip() else "Command completed successfully."
                        )
                except Exception as e:
                    error_msg = f"Error executing '{' '.join(command_parts)}': {str(e)}"
                    output_parts.append(error_msg)
                    logger.error(error_msg)
                    break

            return "\n".join(output_parts)

        except Exception as e:
            return f"Error executing command: {e}"
        finally:
            os.chdir(original_cwd)

    def _get_command_timeout(self, command_parts: List[str]) -> int:
        """Determine appropriate timeout for command type."""
        if not command_parts:
            return 30

        base_command = command_parts[0]

        # Extended timeouts for package management operations
        if base_command in ("pip", "pip3"):
            return 120  # 2 minutes for pip operations
        elif base_command in ("python", "python3"):
            return 60  # 1 minute for python scripts
        else:
            return 30  # 30 seconds for other commands
