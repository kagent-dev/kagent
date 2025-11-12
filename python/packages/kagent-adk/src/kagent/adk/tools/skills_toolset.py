from __future__ import annotations

import logging
from pathlib import Path
from typing import List, Optional

try:
    from typing_extensions import override
except ImportError:
    from typing import override

from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.tools import BaseTool
from google.adk.tools.base_toolset import BaseToolset

from ..tools import BashTool, EditFileTool, ReadFileTool, WriteFileTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


class SkillsToolset(BaseToolset):
    """Toolset that provides Skills functionality for domain expertise execution.

    This toolset provides skills access through specialized tools:
    1. SkillsTool - Discover and load skill instructions
    2. ReadFileTool - Read files with line numbers
    3. WriteFileTool - Write/create files
    4. EditFileTool - Edit files with precise replacements
    5. BashTool - Execute shell commands

    Skills provide specialized domain knowledge and scripts that the agent can use
    to solve complex tasks. The toolset enables discovery of available skills,
    file manipulation, and command execution.

    Note: For file upload/download, use the ArtifactsToolset separately.
    """

    def __init__(self, skills_directory: str | Path):
        """Initialize the skills toolset.

        Args:
          skills_directory: Path to directory containing skill folders.
        """
        super().__init__()
        self.skills_directory = Path(skills_directory)

        # Create skills tools
        self.skills_tool = SkillsTool(skills_directory)
        self.read_file_tool = ReadFileTool()
        self.write_file_tool = WriteFileTool()
        self.edit_file_tool = EditFileTool()
        self.bash_tool = BashTool(skills_directory)

    @override
    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> List[BaseTool]:
        """Get all skills tools.

        Returns:
          List containing all skills tools: skills, read, write, edit, and bash.
        """
        return [
            self.skills_tool,
            self.read_file_tool,
            self.write_file_tool,
            self.edit_file_tool,
            self.bash_tool,
        ]
