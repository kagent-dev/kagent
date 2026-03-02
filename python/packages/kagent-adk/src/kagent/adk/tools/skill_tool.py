"""Tool for discovering and loading skills."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

from kagent.skills import (
    discover_skills,
    generate_skills_tool_description,
    load_skill_content,
)

logger = logging.getLogger("kagent_adk." + __name__)


class SkillsTool(BaseTool):
    """Discover and load skill instructions.

    This tool dynamically discovers available skills and embeds their metadata in the
    tool description. Agent invokes a skill by name to load its full instructions.
    """

    def __init__(self, skills_directory: str | Path):
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

        self._skill_cache: Dict[str, str] = {}

        # Generate description with available skills embedded
        description = self._generate_description_with_skills()

        super().__init__(
            name="skills",
            description=description,
        )

    def _generate_description_with_skills(self) -> str:
        """Generate tool description with available skills embedded."""
        skills = discover_skills(self.skills_directory)
        return generate_skills_tool_description(skills)

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "command": types.Schema(
                        type=types.Type.STRING,
                        description='The skill name (no arguments). E.g., "data-analysis" or "pdf-processing"',
                    ),
                },
                required=["command"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Execute skill loading by name."""
        skill_name = args.get("command", "").strip()

        if not skill_name:
            return "Error: No skill name provided"

        return self._invoke_skill(skill_name)

    def _invoke_skill(self, skill_name: str) -> str:
        """Load and return the full content of a skill."""
        # Check cache first
        if skill_name in self._skill_cache:
            return self._skill_cache[skill_name]

        try:
            content = load_skill_content(self.skills_directory, skill_name)
            formatted_content = self._format_skill_content(skill_name, content)

            # Cache the formatted content
            self._skill_cache[skill_name] = formatted_content

            return formatted_content
        except (FileNotFoundError, IOError) as e:
            logger.error(f"Failed to load skill {skill_name}: {e}")
            return f"Error loading skill '{skill_name}': {e}"
        except Exception as e:
            logger.error(f"An unexpected error occurred while loading skill {skill_name}: {e}")
            return f"An unexpected error occurred while loading skill '{skill_name}': {e}"

    def _format_skill_content(self, skill_name: str, content: str) -> str:
        """Format skill content for display to the agent."""
        header = (
            f'<command-message>The "{skill_name}" skill is loading</command-message>\n\n'
            f"Base directory for this skill: {self.skills_directory}/{skill_name}\n\n"
        )
        footer = (
            "\n\n---\n"
            "The skill has been loaded. Follow the instructions above and use the bash tool to execute commands."
        )
        return header + content + footer
