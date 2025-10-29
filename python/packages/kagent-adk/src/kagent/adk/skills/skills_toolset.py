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

from .bash_tool import BashTool
from .skill_tool import SkillsTool

logger = logging.getLogger("kagent_adk." + __name__)


class SkillsToolset(BaseToolset):
    """Toolset that provides Skills functionality for domain expertise execution.

    This toolset provides skills access through two focused tools:
    1. SkillsTool - Discover and load skill instructions
    2. BashTool - Execute commands based on skill guidance

    Skills provide specialized domain knowledge and scripts that the agent can use
    to solve complex tasks. The toolset enables discovery of available skills and
    execution of skill-based commands.

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
        self.bash_tool = BashTool(skills_directory)

    @override
    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> List[BaseTool]:
        """Get both skills tools.

        Returns:
          List containing SkillsTool and BashTool.
        """
        return [
            self.skills_tool,
            self.bash_tool,
        ]
