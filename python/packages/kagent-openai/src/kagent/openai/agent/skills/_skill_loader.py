"""Skill loading and parsing utilities.

This module handles parsing SKILL.md files and discovering skills from the filesystem.
"""

from __future__ import annotations

import logging
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml
from agents.exceptions import UserError

logger = logging.getLogger("kagent.openai.agent.skills")


@dataclass
class SkillMetadata:
    """Metadata extracted from a SKILL.md file's YAML frontmatter."""

    name: str
    """The name of the skill in hyphen-case."""

    description: str
    """Description of what the skill does and when to use it."""

    license: str | None = None
    """Optional license information."""

    allowed_tools: list[str] | None = None
    """Optional list of tools that are pre-approved to run."""

    metadata: dict[str, str] | None = None
    """Optional additional metadata key-value pairs."""


@dataclass
class Skill:
    """A skill is a folder containing instructions, scripts, and resources.

    Skills follow the Agent Skills specification with a SKILL.md file containing
    YAML frontmatter and markdown instructions.
    """

    metadata: SkillMetadata
    """Skill metadata from YAML frontmatter."""

    skill_path: Path
    """Path to the skill directory."""

    instructions: str
    """The markdown body of the SKILL.md file (excluding frontmatter)."""

    def get_summary(self) -> str:
        """Get a one-line summary of the skill for inclusion in prompts.

        Returns:
            String in format: "- [name]: description"
        """
        return f"- [{self.metadata.name}]: {self.metadata.description}"

    def read_file(self, relative_path: str) -> str:
        """Read a file bundled with the skill.

        Args:
            relative_path: Path relative to the skill directory (e.g. "references/api.md")

        Returns:
            File contents as string.

        Raises:
            FileNotFoundError: If the file doesn't exist.
            UserError: If attempting to read outside skill directory.
        """
        file_path = self.skill_path / relative_path

        # Security: Ensure we're not reading outside the skill directory
        try:
            file_path = file_path.resolve()
            skill_path_resolved = self.skill_path.resolve()
            file_path.relative_to(skill_path_resolved)
        except ValueError:
            raise UserError(f"Cannot read file outside skill directory: {relative_path}")

        if not file_path.exists():
            raise FileNotFoundError(f"File not found in skill '{self.metadata.name}': {relative_path}")

        return file_path.read_text(encoding="utf-8")

    def list_files(self, pattern: str = "*") -> list[str]:
        """List files in the skill directory matching a pattern.

        Args:
            pattern: Glob pattern (e.g., "scripts/*.py", "references/*.md")

        Returns:
            List of file paths relative to the skill directory.
        """
        matches = self.skill_path.glob(pattern)
        return [str(p.relative_to(self.skill_path)) for p in matches if p.is_file() and p.name != "SKILL.md"]


class SkillLoader:
    """Utilities for loading and parsing skills from the filesystem."""

    FRONTMATTER_PATTERN = re.compile(
        r"^---\s*\n(.*?)\n---\s*\n(.*)",
        re.DOTALL,
    )

    @staticmethod
    def load_skill(skill_path: Path) -> Skill:
        """Load a skill from a directory containing a SKILL.md file.

        Args:
            skill_path: Path to the skill directory.

        Returns:
            Parsed Skill object.

        Raises:
            FileNotFoundError: If SKILL.md doesn't exist.
            UserError: If SKILL.md is malformed.
        """
        if not skill_path.is_dir():
            raise UserError(f"Skill path must be a directory: {skill_path}")

        skill_md_path = skill_path / "SKILL.md"
        if not skill_md_path.exists():
            raise FileNotFoundError(f"SKILL.md not found in directory: {skill_path}")

        content = skill_md_path.read_text(encoding="utf-8")
        metadata, instructions = SkillLoader._parse_skill_md(content, skill_path)

        # Validate that directory name matches skill name
        if skill_path.name != metadata.name:
            logger.warning(
                f"Skill directory name '{skill_path.name}' does not match skill name '{metadata.name}' in SKILL.md"
            )

        return Skill(
            metadata=metadata,
            skill_path=skill_path,
            instructions=instructions,
        )

    @staticmethod
    def _parse_skill_md(content: str, skill_path: Path) -> tuple[SkillMetadata, str]:
        """Parse SKILL.md content into metadata and instructions.

        Args:
            content: Full content of SKILL.md file.
            skill_path: Path to skill directory (for error messages).

        Returns:
            Tuple of (metadata, instructions).

        Raises:
            UserError: If SKILL.md is malformed.
        """
        match = SkillLoader.FRONTMATTER_PATTERN.match(content)
        if not match:
            raise UserError(
                f"SKILL.md in {skill_path} must start with YAML frontmatter (---\\nname: ...\\ndescription: ...\\n---)"
            )

        frontmatter_str = match.group(1)
        instructions = match.group(2).strip()

        try:
            frontmatter: dict[str, Any] = yaml.safe_load(frontmatter_str)
        except yaml.YAMLError as e:
            raise UserError(f"Invalid YAML frontmatter in {skill_path}/SKILL.md: {e}") from e

        if not isinstance(frontmatter, dict):
            raise UserError(f"YAML frontmatter in {skill_path}/SKILL.md must be a dictionary")

        # Validate required fields
        name = frontmatter.get("name")
        description = frontmatter.get("description")

        if not name or not isinstance(name, str):
            raise UserError(f"SKILL.md in {skill_path} must have a 'name' field (string)")

        if not description or not isinstance(description, str):
            raise UserError(f"SKILL.md in {skill_path} must have a 'description' field (string)")

        # Validate name format (lowercase alphanumeric + hyphens)
        if not re.match(r"^[a-z0-9-]+$", name):
            raise UserError(f"Skill name must be lowercase alphanumeric with hyphens: '{name}'")

        # Optional fields
        license_info = frontmatter.get("license")
        allowed_tools = frontmatter.get("allowed-tools")
        metadata = frontmatter.get("metadata")

        if allowed_tools is not None and not isinstance(allowed_tools, list):
            raise UserError(f"'allowed-tools' in {skill_path}/SKILL.md must be a list")

        if metadata is not None and not isinstance(metadata, dict):
            raise UserError(f"'metadata' in {skill_path}/SKILL.md must be a dictionary")

        return (
            SkillMetadata(
                name=name,
                description=description,
                license=license_info,
                allowed_tools=allowed_tools,
                metadata=metadata,
            ),
            instructions,
        )

    @staticmethod
    def discover_skills(base_dir: Path) -> list[Skill]:
        """Discover all skills in a directory tree.

        Searches recursively for directories containing SKILL.md files.

        Args:
            base_dir: Root directory to search.

        Returns:
            List of discovered Skill objects.
        """
        if not base_dir.exists():
            raise FileNotFoundError(f"Skills directory not found: {base_dir}")

        if not base_dir.is_dir():
            raise UserError(f"Skills path must be a directory: {base_dir}")

        skills = []
        for skill_md in base_dir.rglob("SKILL.md"):
            skill_dir = skill_md.parent
            try:
                skill = SkillLoader.load_skill(skill_dir)
                skills.append(skill)
                logger.debug(f"Loaded skill: {skill.metadata.name} from {skill_dir}")
            except Exception as e:
                logger.warning(f"Failed to load skill from {skill_dir}: {e}")

        return skills
