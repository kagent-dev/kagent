from __future__ import annotations

import logging
from pathlib import Path
from typing import Dict, List

import yaml

from .models import Skill

logger = logging.getLogger(__name__)


def parse_skill_metadata(skill_file: Path) -> Dict[str, str] | None:
    """Parse YAML frontmatter from a SKILL.md file."""
    try:
        with open(skill_file, encoding="utf-8") as f:
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


def discover_skills(skills_directory: Path) -> List[Skill]:
    """Discover available skills and return their metadata."""
    if not skills_directory.exists():
        logger.warning(f"Skills directory not found: {skills_directory}")
        return []

    skills = []
    for skill_dir in sorted(skills_directory.iterdir()):
        if not skill_dir.is_dir():
            continue

        skill_file = skill_dir / "SKILL.md"
        if not skill_file.exists():
            continue

        try:
            metadata = parse_skill_metadata(skill_file)
            if metadata:
                skills.append(Skill(**metadata))
        except Exception as e:
            logger.error(f"Failed to parse skill {skill_dir.name}: {e}")

    return skills
