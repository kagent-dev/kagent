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
from typing import List, Optional

try:
    from typing_extensions import override
except ImportError:
    from typing import override

from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.base_toolset import BaseToolset
from .skills_shell_tool import SkillsShellTool

logger = logging.getLogger("google_adk." + __name__)


class SkillsToolset(BaseToolset):
    """Toolset that provides shell-based Skills functionality.

    This toolset provides skills access through a single shell tool that agents
    can use for all skills operations including discovery, content loading, and
    script execution using standard shell commands.

    This approach is much simpler than multiple specialized tools and provides
    more flexibility for skills operations.
    """

    def __init__(self, skills_directory: str | Path):
        """Initialize the skills toolset.

        Args:
          skills_directory: Path to directory containing skill folders.
        """
        super().__init__()
        self.skills_directory = Path(skills_directory)

        # Create shell tool for skills operations
        self.shell_tool = SkillsShellTool(skills_directory)

    @override
    async def get_tools(
        self, readonly_context: Optional[ReadonlyContext] = None
    ) -> List[BaseTool]:
        """Get the shell tool for skills operations.

        Returns:
          List containing the skills shell tool.
        """
        return [self.shell_tool]
