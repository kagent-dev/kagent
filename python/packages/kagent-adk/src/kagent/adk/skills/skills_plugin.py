# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import logging
from pathlib import Path
from typing import Optional

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.agents.callback_context import CallbackContext
from google.adk.plugins import BasePlugin
from google.genai import types

from .bash_tool import BashTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


class SkillsPlugin(BasePlugin):
    """Convenience plugin for multi-agent apps to automatically register Skills tools.

    This plugin is purely a convenience wrapper that automatically adds the SkillsTool
    and BashTool to all LLM agents in an application. It does not add any additional
    functionality beyond tool registration.

    For single-agent use cases or when you prefer explicit control, you can skip this plugin
    and directly add both tools to your agent's tools list.

    Example:
        # Without plugin (direct tool usage):
        agent = Agent(
            tools=[
                SkillsTool(skills_directory="./skills"),
                BashTool(skills_directory="./skills"),
            ]
        )

        # With plugin (auto-registration for multi-agent apps):
        app = App(
            root_agent=agent,
            plugins=[SkillsPlugin(skills_directory="./skills")]
        )
    """

    def __init__(self, skills_directory: str | Path, name: str = "skills_plugin"):
        """Initialize the skills plugin.

        Args:
          skills_directory: Path to directory containing skill folders.
          name: Name of the plugin instance.
        """
        super().__init__(name)
        self.skills_directory = Path(skills_directory)

    async def before_agent_callback(
        self, *, agent: BaseAgent, callback_context: CallbackContext
    ) -> Optional[types.Content]:
        """Add skills tools to agents if not already present."""

        add_skills_tool_to_agent(self.skills_directory, agent)


def add_skills_tool_to_agent(skills_directory: str | Path, agent: BaseAgent) -> None:
    """Utility function to add Skills and Bash tools to a given agent.

    Args:
      agent: The LlmAgent instance to which the tools will be added.
      skills_directory: Path to directory containing skill folders.
    """

    if not isinstance(agent, LlmAgent):
        return

    skills_directory = Path(skills_directory)
    existing_tool_names = {getattr(t, "name", None) for t in agent.tools}

    # Add SkillsTool if not already present
    if "skills" not in existing_tool_names:
        agent.tools.append(SkillsTool(skills_directory))
        logger.debug(f"Added skills invoke tool to agent: {agent.name}")

    # Add BashTool if not already present
    if "bash" not in existing_tool_names:
        agent.tools.append(BashTool(skills_directory))
        logger.debug(f"Added bash tool to agent: {agent.name}")
