from __future__ import annotations

import logging
from pathlib import Path

logger = logging.getLogger(__name__)


def load_skill_content(skills_directory: Path, skill_name: str) -> str:
    """Load and return the full content of a skill's SKILL.md file."""
    # Find skill directory
    skill_dir = skills_directory / skill_name
    if not skill_dir.exists() or not skill_dir.is_dir():
        raise FileNotFoundError(f"Skill '{skill_name}' not found in {skills_directory}")

    skill_file = skill_dir / "SKILL.md"
    if not skill_file.exists():
        raise FileNotFoundError(f"Skill '{skill_name}' has no SKILL.md file in {skill_dir}")

    try:
        with open(skill_file, encoding="utf-8") as f:
            content = f.read()
        return content
    except Exception as e:
        logger.error(f"Failed to load skill {skill_name}: {e}")
        raise OSError(f"Error loading skill '{skill_name}': {e}") from e
