"""Tool for discovering and loading skills."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict, Optional

import yaml
from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from pydantic import BaseModel

logger = logging.getLogger("kagent_adk." + __name__)


class Skill(BaseModel):
    """Represents the metadata for a skill.

    This is a simple data container used during the initial skill discovery
    phase to hold the information parsed from a skill's SKILL.md frontmatter.
    """

    name: str
    """The unique name/identifier of the skill."""

    description: str
    """A description of what the skill does and when to use it."""

    license: Optional[str] = None
    """Optional license information for the skill."""


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
        base_description = (
            "Execute a skill within the main conversation\n\n"
            "<skills_instructions>\n"
            "When users ask you to perform tasks, check if any of the available skills below can help "
            "complete the task more effectively. Skills provide specialized capabilities and domain knowledge.\n\n"
            "How to use skills:\n"
            "- Invoke skills using this tool with the skill name only (no arguments)\n"
            "- When you invoke a skill, the skill's full SKILL.md will load with detailed instructions\n"
            "- Follow the skill's instructions and use the bash tool to execute commands\n"
            "- Examples:\n"
            '  - command: "data-analysis" - invoke the data-analysis skill\n'
            '  - command: "pdf-processing" - invoke the pdf-processing skill\n\n'
            "Important:\n"
            "- Only use skills listed in <available_skills> below\n"
            "- Do not invoke a skill that is already loaded in the conversation\n"
            "- After loading a skill, use the bash tool for execution\n"
            "- If not specified, scripts are located in the skill-name/scripts subdirectory\n"
            "</skills_instructions>\n\n"
        )

        # Discover and append available skills
        skills_xml = self._discover_skills()
        return base_description + skills_xml

    def _discover_skills(self) -> str:
        """Discover available skills and format as XML."""
        if not self.skills_directory.exists():
            return "<available_skills>\n<!-- No skills directory found -->\n</available_skills>\n"

        skills_entries = []
        for skill_dir in sorted(self.skills_directory.iterdir()):
            if not skill_dir.is_dir():
                continue

            skill_file = skill_dir / "SKILL.md"
            if not skill_file.exists():
                continue

            try:
                metadata = self._parse_skill_metadata(skill_file)
                if metadata:
                    skill_xml = (
                        "<skill>\n"
                        f"<name>{metadata['name']}</name>\n"
                        f"<description>{metadata['description']}</description>\n"
                        "</skill>"
                    )
                    skills_entries.append(skill_xml)
            except Exception as e:
                logger.error(f"Failed to parse skill {skill_dir.name}: {e}")

        if not skills_entries:
            return "<available_skills>\n<!-- No skills found -->\n</available_skills>\n"

        return "<available_skills>\n" + "\n".join(skills_entries) + "\n</available_skills>\n"

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

        # Find skill directory
        skill_dir = self.skills_directory / skill_name
        if not skill_dir.exists() or not skill_dir.is_dir():
            return f"Error: Skill '{skill_name}' not found. Check the available skills list in the tool description."

        skill_file = skill_dir / "SKILL.md"
        if not skill_file.exists():
            return f"Error: Skill '{skill_name}' has no SKILL.md file."

        try:
            with open(skill_file, "r", encoding="utf-8") as f:
                content = f.read()

            formatted_content = self._format_skill_content(skill_name, content)

            # Cache the formatted content
            self._skill_cache[skill_name] = formatted_content

            return formatted_content

        except Exception as e:
            logger.error(f"Failed to load skill {skill_name}: {e}")
            return f"Error loading skill '{skill_name}': {e}"

    def _parse_skill_metadata(self, skill_file: Path) -> Dict[str, str] | None:
        """Parse YAML frontmatter from a SKILL.md file."""
        try:
            with open(skill_file, "r", encoding="utf-8") as f:
                content = f.read()

            if not content.startswith("---"):
                return None

            parts = content.split("---", 2)
            if len(parts) < 3:
                return None

            metadata = yaml.safe_load(parts[1])
            if isinstance(metadata, dict) and "name" in metadata and "description" in metadata:
                return {
                    "name": metadata["name"],
                    "description": metadata["description"],
                }
            return None
        except Exception as e:
            logger.error(f"Failed to parse metadata from {skill_file}: {e}")
            return None

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
