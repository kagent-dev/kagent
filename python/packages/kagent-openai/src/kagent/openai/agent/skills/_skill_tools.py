"""Function tools for interacting with skills.

This module provides pre-built tools that agents can use to discover and load skills.
"""

from __future__ import annotations

from agents.run_context import RunContextWrapper
from agents.tool import FunctionTool, function_tool

from ._skill_registry import SkillRegistry


def get_skill_tool(registry: SkillRegistry) -> FunctionTool:
    """Create a Skill tool with skills from the given registry.

    This function generates a tool instance with skills from the provided registry
    embedded in its description, following Anthropic's pattern.

    Args:
        registry: SkillRegistry instance containing registered skills.

    Returns:
        A FunctionTool configured with the skills from the registry.

    Example:
        ```python
        from agents.agent import Agent
        from kagent.openai.agent.skills import SkillRegistry, get_skill_tool

        # Create registry and register skills
        registry = SkillRegistry()
        registry.register_skill_directory("./my-skills")

        # Get tool with skills from this registry
        skill_tool = get_skill_tool(registry)

        agent = Agent(
            name="Assistant",
            instructions="You are a helpful assistant. Use skills when appropriate.",
            tools=[skill_tool]
        )
        ```
    """
    skills_xml = registry.get_skills_xml()

    description = f"""Load specialized skill instructions for completing complex tasks.

When you invoke this tool, it loads detailed step-by-step instructions for how to complete
a specific type of task. After loading a skill, you must execute its instructions using your
other available tools (bash, read_file, write_file, edit_file).

WORKFLOW:
1. Call this tool with the skill name
2. Receive detailed instructions
3. Execute those instructions step-by-step using your other tools
4. Complete the user's request

Available skills:
{skills_xml}

NOTE: This tool loads instructions - you must then execute them. Do not stop after calling
this tool; immediately begin following the instructions it provides."""

    @function_tool(name_override="Skill", description_override=description)
    def skill_tool_impl(context: RunContextWrapper, command: str) -> str:
        """Execute a skill by name.

        Args:
            command: The name of the skill to execute (e.g., "pdf", "mcp-builder")

        Returns:
            The full skill instructions and context.
        """
        skill_name = command.strip()
        skill = registry.get_skill(skill_name)

        if skill is None:
            available: str = ", ".join(registry.get_skill_names())
            return (
                f"Error: Skill '{skill_name}' not found.\n\n"
                f"Available skills: {available}\n\n"
                "Use the exact skill name from the available skills list."
            )

        # Return in the format matching Claude Code's implementation
        base_dir = skill.skill_path.resolve()

        # Format with an explicit prompt at the end to force continuation
        return f"""Launching skill: {skill_name}

Base directory for this skill: {base_dir}

{skill.instructions}

---
Now execute the above instructions step-by-step. Start by following the first instruction."""

    return skill_tool_impl
