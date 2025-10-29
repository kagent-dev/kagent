"""Basic example demonstrating Skills support in the OpenAI Agents SDK.

This example shows how to:
1. Register skills from a directory
2. Create an agent with the Skill tool
3. Use dynamic instructions to inject skill metadata
4. Let the agent discover and use skills

Skills provide specialized knowledge and workflows that agents can load on-demand,
following the progressive disclosure pattern from Anthropic's Skills architecture.
"""

import asyncio
import uuid
from pathlib import Path
from typing import Any
from agents import (
    Agent,
    MessageOutputItem,
    RawResponsesStreamEvent,
    ReasoningItem,
    RunItemStreamEvent,
    Runner,
    ToolCallItem,
    ToolCallOutputItem,
)
from agents.items import ItemHelpers
from agents.memory.sqlite_session import SQLiteSession
from kagent.openai.agents.skills import SkillRegistry, get_skill_tool
from kagent.openai.agents.tools import SRT_SHELL_TOOL
from openai.types.responses import ResponseFunctionCallArgumentsDeltaEvent


def create_agent_with_skills(registry: SkillRegistry) -> Agent:
    """Create an agent configured to use skills.

    The Skill tool's description includes all available skills from the registry.
    The instructions just provide general guidance about when to use skills.

    Args:
        registry: SkillRegistry instance with registered skills.

    Returns:
        Agent configured with skills support.
    """
    # Get the skill tool with skills from the registry embedded in its description
    skill_tool = get_skill_tool(registry)

    instructions = """You are a helpful AI assistant with access to specialized skills and tools.

When a user's request matches a skill's capabilities:
1. Use the Skill tool to load that skill's instructions
2. The skill will provide detailed step-by-step instructions for how to complete the task
3. Follow those instructions using your available tools (bash, read_file, write_file, edit_file)
4. Complete the entire workflow described in the skill

The skill instructions are your detailed implementation guide - execute them fully."""

    agent = Agent(
        name="Assistant with Skills",
        instructions=instructions,
        tools=[
            skill_tool,
            SRT_SHELL_TOOL,
        ],
        # Default model, aka gpt-4.1 doesn't work with skills
        model="gpt-4o",
    )

    return agent


async def main():
    """Main example demonstrating skills usage with streaming."""
    print("=" * 70)
    print("OpenAI Agents SDK - Skills Example (Interactive)")
    print("=" * 70)
    print()

    # Step 1: Create a skill registry
    registry = SkillRegistry()
    print("✓ Created skill registry")
    print()

    # Step 2: Register skills from a directory
    # In a real application, you'd point this to your skills directory
    skills_dir = Path("~/src/anthropics/skills").expanduser().resolve()
    if not skills_dir.exists():
        raise FileNotFoundError(f"Skills directory not found: {skills_dir}")
    num_skills = registry.register_skill_directory(skills_dir)
    print(f"✓ Registered {num_skills} skill(s) from: {skills_dir}")
    print()

    # Step 3: Create agent
    agent = create_agent_with_skills(registry)
    print("✓ Created agent with Skills support")
    print()

    print("You can now chat with the agent. Type 'quit' or 'exit' to end.")
    print("=" * 70)
    print()

    # generate a random session id
    session_id = str(uuid.uuid4())[:8]
    sqlite_session = SQLiteSession(session_id)

    # Interactive loop
    while True:
        try:
            user_input = input("You: ").strip()
            if not user_input:
                continue

            if user_input.lower() in ["quit", "exit", "bye"]:
                print("\nGoodbye!")
                break

            print("\nAgent:\n")
            # Stream the response
            result = Runner.run_streamed(agent, user_input, max_turns=100, session=sqlite_session)
            # Track function calls for detailed output
            function_calls: dict[Any, dict[str, Any]] = {}  # call_id -> {name, arguments}
            current_active_call_id = None

            async for event in result.stream_events():
                if event.type == "raw_response_event":
                    # Function call started
                    if event.data.type == "response.output_item.added":
                        if getattr(event.data.item, "type", None) == "function_call":
                            function_name = getattr(event.data.item, "name", "unknown")
                            call_id = getattr(event.data.item, "call_id", "unknown")

                            function_calls[call_id] = {"name": function_name, "arguments": ""}
                            current_active_call_id = call_id
                            print(f"\n📞 Function call streaming started: {function_name}()")
                            print("📝 Arguments building...")

                    # Real-time argument streaming
                    elif isinstance(event.data, ResponseFunctionCallArgumentsDeltaEvent):
                        if current_active_call_id and current_active_call_id in function_calls:
                            function_calls[current_active_call_id]["arguments"] += event.data.delta
                            print(event.data.delta, end="", flush=True)

                    # Function call completed
                    elif event.data.type == "response.output_item.done":
                        if hasattr(event.data.item, "call_id"):
                            call_id = getattr(event.data.item, "call_id", "unknown")
                            if call_id in function_calls:
                                function_info = function_calls[call_id]
                                print(f"\n✅ Function call streaming completed: {function_info['name']}")
                                print()
                                if current_active_call_id == call_id:
                                    current_active_call_id = None
                # Handle raw streaming events (reasoning and text deltas)
                if isinstance(event, RawResponsesStreamEvent):
                    if event.data.type == "response.reasoning_text.delta":
                        print(f"\033[90m{event.data.delta}\033[0m", end="", flush=True)
                    elif event.data.type == "response.output_text.delta":
                        print(event.data.delta, end="", flush=True)
                    elif event.data.type == "response.output_text.done":
                        print()  # New line after text output completes

        except KeyboardInterrupt:
            print("\n\nInterrupted. Type 'quit' to exit.")
        except Exception as e:
            print(f"\nError: {e}\n")


if __name__ == "__main__":
    asyncio.run(main())
