"""Agent Skills support for the OpenAI Agents SDK.

Skills are folders of instructions, scripts, and resources that agents can discover
and load dynamically to perform better at specific tasks. This module provides
utilities for discovering, loading, and using skills with agents.

Skills follow Anthropic's Agent Skills specification with progressive disclosure:
- Level 1: Skill metadata (name + description) always in system prompt
- Level 2: Full SKILL.md content loaded when skill is activated via Skill tool
- Level 3: Additional files accessible from skill base directory

Example:
    ```python
    from agents import Agent, Runner, SkillRegistry, get_skill_tool

    # Create registry and register skills
    registry = SkillRegistry()
    registry.register_skill_directory("./my-skills")

    # Get skill tool with skills from this registry
    skill_tool = get_skill_tool(registry)

    # Create agent with skill support
    agent = Agent(
        name="Assistant",
        instructions="You are a helpful assistant. Use skills when appropriate.",
        tools=[skill_tool]
    )

    result = await Runner.run(agent, "Help me work with PDFs")
    ```
"""

from ._skill_loader import Skill, SkillLoader, SkillMetadata
from ._skill_registry import SkillRegistry
from ._skill_tools import get_skill_tool

__all__ = [
    "Skill",
    "SkillLoader",
    "SkillMetadata",
    "SkillRegistry",
    "get_skill_tool",
]
