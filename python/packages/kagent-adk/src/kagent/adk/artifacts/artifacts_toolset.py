from __future__ import annotations

import logging
from typing import List, Optional

try:
    from typing_extensions import override
except ImportError:
    from typing import override

from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.tools import BaseTool
from google.adk.tools.base_toolset import BaseToolset

from .return_artifacts_tool import ReturnArtifactsTool
from .stage_artifacts_tool import StageArtifactsTool

logger = logging.getLogger("kagent_adk." + __name__)


class ArtifactsToolset(BaseToolset):
    """Toolset for managing artifact upload and download workflows.

    This toolset provides tools for the complete artifact lifecycle:
    1. StageArtifactsTool - Download artifacts from artifact service to working directory
    2. ReturnArtifactsTool - Upload generated files from working directory to artifact service

    Artifacts enable file-based interactions:
    - Users upload files via frontend → stored as artifacts
    - StageArtifactsTool copies them to working directory for processing
    - Processing tools (bash, skills, etc.) work with files on disk
    - ReturnArtifactsTool saves generated outputs back as artifacts
    - Users download results via frontend

    This toolset is independent of skills and can be used with any processing workflow.
    """

    def __init__(self):
        """Initialize the artifacts toolset."""
        super().__init__()

        # Create artifact lifecycle tools
        self.stage_artifacts_tool = StageArtifactsTool()
        self.return_artifacts_tool = ReturnArtifactsTool()

    @override
    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> List[BaseTool]:
        """Get both artifact tools.

        Returns:
          List containing StageArtifactsTool and ReturnArtifactsTool.
        """
        return [
            self.stage_artifacts_tool,
            self.return_artifacts_tool,
        ]
