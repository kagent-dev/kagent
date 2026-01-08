from __future__ import annotations

from pydantic import BaseModel


class Skill(BaseModel):
    """Represents the metadata for a skill.

    This is a simple data container used during the initial skill discovery
    phase to hold the information parsed from a skill's SKILL.md frontmatter.
    """

    name: str
    """The unique name/identifier of the skill."""

    description: str
    """A description of what the skill does and when to use it."""

    license: str | None = None
    """Optional license information for the skill."""
