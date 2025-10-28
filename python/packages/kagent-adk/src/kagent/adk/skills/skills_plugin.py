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
from typing import Dict, List, Optional

from google.genai import types
import yaml

from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.callback_context import CallbackContext
from google.adk.plugins.base_plugin import BasePlugin
from .skills_shell_tool import SkillsShellTool

logger = logging.getLogger("google_adk." + __name__)


class SkillsPlugin(BasePlugin):
    """Plugin that provides shell-based Skills functionality.

    This plugin provides global skills access by:
    1. Adding a shell tool for skills operations to all LLM agents.
    2. Injecting a "Level 1" discovery prompt (names and descriptions of
       available skills) into the agent's instructions.
    """

    def __init__(self, skills_directory: str | Path, name: str = "skills_plugin"):
        """Initialize the skills plugin.

        Args:
          skills_directory: Path to directory containing skill folders.
          name: Name of the plugin instance.
        """
        super().__init__(name)
        self.skills_directory = Path(skills_directory)
        self.shell_tool = SkillsShellTool(skills_directory)
        self._skill_metadata = self._parse_skill_metadata()

    def _parse_skill_metadata(self) -> List[Dict[str, str]]:
        """Parse the YAML frontmatter of all SKILL.md files."""
        metadata_list = []
        if not self.skills_directory.exists():
            logger.warning(f"Skills directory not found: {self.skills_directory}")
            return metadata_list

        for skill_dir in self.skills_directory.iterdir():
            skill_file = skill_dir / "SKILL.md"
            if not (skill_dir.is_dir() and skill_file.exists()):
                continue

            try:
                with open(skill_file, "r", encoding="utf-8") as f:
                    content = f.read()

                if not content.startswith("---"):
                    continue

                parts = content.split("---", 2)
                if len(parts) < 3:
                    continue

                metadata = yaml.safe_load(parts[1])
                if isinstance(metadata, dict) and "name" in metadata and "description" in metadata:
                    metadata_list.append({
                        "name": metadata["name"],
                        "description": metadata["description"]
                    })
            except Exception as e:
                logger.error(f"Failed to parse metadata for skill in {skill_dir}: {e}")
        return metadata_list

    async def before_agent_callback(
        self, *, agent: BaseAgent, callback_context: CallbackContext
    ) -> Optional[types.Content]:
        """Add shell tool and skills context to agents."""
        from ..agents.llm_agent import LlmAgent
        if not isinstance(agent, LlmAgent):
            return None

        # 1. Add shell tool if not already present
        if "shell" not in {getattr(t, "name", None) for t in agent.tools}:
             agent.tools.append(self.shell_tool)
             logger.debug(f"Added shell tool to agent: {agent.name}")

        # 2. Add skills context to agent instruction
        if hasattr(agent, "instruction") and isinstance(agent.instruction, str):
            if "## Available Skills" not in agent.instruction:
                skills_context = self._generate_skills_instruction()
                agent.instruction += skills_context
                logger.debug(f"Enhanced agent instruction for agent: {agent.name}")

        callback_context.state["skills_available"] = True
        return None

    def _generate_skills_instruction(self) -> str:
        """Generate the concise 'Level 1' skills instruction."""
        if not self._skill_metadata:
            return ""

        lines = ["## Available Skills",
                 "You have access to the following specialized skills. Use the `shell` tool to interact with them.",
                 "---"]
        for metadata in self._skill_metadata:
            lines.append(f"- **{metadata['name']}**: {metadata['description']}")
        lines.append("---")
        lines.append("To use a skill, read its instructions with `shell(\"cat skills/<skill-name>/SKILL.md\")`.")

        return "\n".join(lines)
