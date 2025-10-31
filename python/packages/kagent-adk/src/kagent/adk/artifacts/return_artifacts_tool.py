"""Tool for returning generated files from working directory to artifact service."""

from __future__ import annotations

import logging
import mimetypes
from pathlib import Path
from typing import Any, Dict, List

from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from typing_extensions import override

from .session_path import get_session_path
from .stage_artifacts_tool import MAX_ARTIFACT_SIZE_BYTES

logger = logging.getLogger("kagent_adk." + __name__)


class ReturnArtifactsTool(BaseTool):
    """Save generated files from working directory to artifact service for user download.

    This tool enables users to download outputs generated during processing.
    Files are saved to the artifact service where they can be retrieved by the frontend.
    """

    def __init__(self):
        super().__init__(
            name="return_artifacts",
            description=(
                "Save generated files from the working directory to the artifact service, "
                "making them available for user download.\n\n"
                "WORKFLOW:\n"
                "1. Generate output files in the 'outputs/' directory\n"
                "2. Use this tool to save those files to the artifact service\n"
                "3. Users can then download the files via the frontend\n\n"
                "USAGE EXAMPLE:\n"
                "- bash('python scripts/analyze.py > outputs/report.txt')\n"
                "- return_artifacts(file_paths=['outputs/report.txt'])\n"
                "  Returns: 'Saved 1 file(s): report.txt (v0, 15.2 KB)'\n\n"
                "PARAMETERS:\n"
                "- file_paths: List of relative paths from working directory (required)\n"
                "- artifact_names: Optional custom names for artifacts (default: use filename)\n\n"
                "BEST PRACTICES:\n"
                "- Generate outputs in 'outputs/' directory for clarity\n"
                "- Use descriptive filenames (they become artifact names)\n"
                "- Return all outputs at once for efficiency"
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration | None:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_paths": types.Schema(
                        type=types.Type.ARRAY,
                        description=(
                            "List of relative file paths from the working directory to save as artifacts. "
                            "Example: ['outputs/report.pdf', 'outputs/data.csv']. "
                            "Files must exist in the working directory and be within size limits."
                        ),
                        items=types.Schema(type=types.Type.STRING),
                    ),
                    "artifact_names": types.Schema(
                        type=types.Type.ARRAY,
                        description=(
                            "Optional custom names for the artifacts. "
                            "If not provided, the filename will be used. "
                            "Must match the length of file_paths if provided."
                        ),
                        items=types.Schema(type=types.Type.STRING),
                    ),
                },
                required=["file_paths"],
            ),
        )

    @override
    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        file_paths: List[str] = args.get("file_paths", [])
        artifact_names: List[str] = args.get("artifact_names", [])

        if not file_paths:
            return "Error: No file paths provided."

        if artifact_names and len(artifact_names) != len(file_paths):
            return "Error: artifact_names length must match file_paths length."

        if not tool_context._invocation_context.artifact_service:
            return "Error: Artifact service is not available in this context."

        try:
            working_dir = get_session_path(session_id=tool_context.session.id)

            saved_artifacts = []
            for idx, rel_path in enumerate(file_paths):
                file_path = (working_dir / rel_path).resolve()

                # Security: Ensure file is within working directory
                if not file_path.is_relative_to(working_dir):
                    logger.warning(f"Skipping file outside working directory: {rel_path}")
                    continue

                # Check file exists
                if not file_path.exists():
                    logger.warning(f"File not found: {rel_path}")
                    continue

                # Check file size
                file_size = file_path.stat().st_size
                if file_size > MAX_ARTIFACT_SIZE_BYTES:
                    size_mb = file_size / (1024 * 1024)
                    logger.warning(f"File too large: {rel_path} ({size_mb:.1f} MB)")
                    continue

                # Determine artifact name
                artifact_name = artifact_names[idx] if artifact_names else file_path.name

                # Read file data and detect MIME type
                file_data = file_path.read_bytes()
                mime_type = self._detect_mime_type(file_path)

                # Create artifact Part
                artifact_part = types.Part.from_bytes(data=file_data, mime_type=mime_type)

                # Save to artifact service
                version = await tool_context.save_artifact(
                    filename=artifact_name,
                    artifact=artifact_part,
                )

                size_kb = file_size / 1024
                saved_artifacts.append(f"{artifact_name} (v{version}, {size_kb:.1f} KB)")
                logger.info(f"Saved artifact: {artifact_name} v{version} ({size_kb:.1f} KB)")

            if not saved_artifacts:
                return "No valid files were saved as artifacts."

            return f"Saved {len(saved_artifacts)} file(s) for download:\n" + "\n".join(
                f"  • {artifact}" for artifact in saved_artifacts
            )

        except Exception as e:
            logger.error("Error returning artifacts: %s", e, exc_info=True)
            return f"An error occurred while returning artifacts: {e}"

    def _detect_mime_type(self, file_path: Path) -> str:
        """Detect MIME type from file extension.

        Args:
            file_path: Path to the file

        Returns:
            MIME type string, defaults to 'application/octet-stream' if unknown
        """
        mime_type, _ = mimetypes.guess_type(str(file_path))
        return mime_type or "application/octet-stream"
