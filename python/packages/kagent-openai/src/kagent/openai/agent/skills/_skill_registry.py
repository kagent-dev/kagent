"""Registry for managing discovered skills.

The SkillRegistry maintains a collection of available skills and provides
utilities for querying and accessing them.
"""

from __future__ import annotations

import logging
from pathlib import Path

from ._skill_loader import Skill, SkillLoader

logger = logging.getLogger("kagent.openai.agent.skills")


class SkillRegistry:
    """Registry for discovered skills.

    This class maintains a collection of skills that can be accessed by agents.
    Create an instance and register skills, then pass it to get_skill_tool().
    """

    def __init__(self) -> None:
        """Initialize an empty skill registry."""
        self._skills: dict[str, Skill] = {}

    def register_skill(self, skill: Skill) -> None:
        """Register a single skill.

        Args:
            skill: Skill object to register.
        """
        if skill.metadata.name in self._skills:
            logger.warning(
                f"Skill '{skill.metadata.name}' is already registered. Overwriting with skill from {skill.skill_path}"
            )

        self._skills[skill.metadata.name] = skill
        logger.debug(f"Registered skill: {skill.metadata.name}")

    def register_skill_directory(self, path: Path | str) -> int:
        """Discover and register all skills in a directory.

        Args:
            path: Path to directory containing skills.

        Returns:
            Number of skills registered.
        """
        path = Path(path)
        skills = SkillLoader.discover_skills(path)

        for skill in skills:
            self.register_skill(skill)

        logger.info(f"Registered {len(skills)} skill(s) from {path}")
        return len(skills)

    def get_skill(self, name: str) -> Skill | None:
        """Get a skill by name.

        Args:
            name: Skill name (e.g., "pdf", "mcp-builder").

        Returns:
            Skill object if found, None otherwise.
        """
        return self._skills.get(name)

    def get_all_skills(self) -> list[Skill]:
        """Get all registered skills.

        Returns:
            List of all registered Skill objects.
        """
        return list(self._skills.values())

    def get_skill_names(self) -> list[str]:
        """Get names of all registered skills.

        Returns:
            List of skill names.
        """
        return list(self._skills.keys())

    def clear(self) -> None:
        """Clear all registered skills.

        Useful for testing or resetting state.
        """
        self._skills.clear()
        logger.debug("Cleared skill registry")

    def get_skills_summary(self) -> str:
        """Get a formatted summary of all registered skills.

        This returns a multi-line string suitable for inclusion in agent instructions,
        with each skill on its own line in format: "- [name]: description"

        Returns:
            Formatted string with all skill summaries.
        """
        if not self._skills:
            return "(No skills registered)"

        return "\n".join(skill.get_summary() for skill in self._skills.values())

    def get_skills_xml(self) -> str:
        """Get skills formatted as XML for inclusion in tool descriptions.

        This returns skills in Anthropic's XML format for the Skill tool description.

        Returns:
            XML-formatted string with all skills.
        """
        if not self._skills:
            return "<available_skills>\n(No skills registered)\n</available_skills>"

        skills_xml = ["<available_skills>"]
        for skill in self._skills.values():
            skills_xml.append("<skill>")
            skills_xml.append(f"<name>{skill.metadata.name}</name>")
            skills_xml.append(f"<description>{skill.metadata.description}</description>")
            skills_xml.append("<location>local</location>")
            skills_xml.append("</skill>")
        skills_xml.append("</available_skills>")

        return "\n".join(skills_xml)

    def get_skill_context(self, skill_name: str) -> str:
        """Get the full context (instructions) for a skill.

        This is Level 2 of progressive disclosure - the full SKILL.md content.

        Args:
            skill_name: Name of the skill.

        Returns:
            Full skill instructions.

        Raises:
            ValueError: If skill is not found.
        """
        skill = self.get_skill(skill_name)
        if skill is None:
            available = ", ".join(self.get_skill_names())
            raise ValueError(f"Skill '{skill_name}' not found. Available skills: {available}")

        return f"# Skill: {skill.metadata.name}\n\n{skill.instructions}"
