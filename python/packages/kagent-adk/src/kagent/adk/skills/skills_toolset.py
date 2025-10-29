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
    """Toolset that provides Skills functionality through two focused tools.

    This toolset provides skills access through two complementary tools following
    progressive disclosure:
    1. SkillsTool - Discover and load skill instructions
    2. BashTool - Execute commands based on skill guidance

    This separation provides clear semantic distinction between skill discovery
    (what can I do?) and skill execution (how do I do it?).
    """

    def __init__(self, skills_directory: str | Path):
        """Initialize the skills toolset.

        Args:
          skills_directory: Path to directory containing skill folders.
        """
        super().__init__()
        self.skills_directory = Path(skills_directory)

        # Create the two tools for skills operations
        self.skills_invoke_tool = SkillsTool(skills_directory)
        self.bash_tool = BashTool(skills_directory)

    @override
    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> List[BaseTool]:
        """Get both skills tools.

        Returns:
          List containing SkillsTool and BashTool.
        """
        return [self.skills_invoke_tool, self.bash_tool]
