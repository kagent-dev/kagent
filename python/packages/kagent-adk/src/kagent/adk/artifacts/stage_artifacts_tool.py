from __future__ import annotations

import logging
import mimetypes
from pathlib import Path
from typing import Any, List

from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from typing_extensions import override

from .session_path import get_session_path

logger = logging.getLogger("kagent_adk." + __name__)

# Maximum file size for staging (100 MB)
MAX_ARTIFACT_SIZE_BYTES = 100 * 1024 * 1024


class StageArtifactsTool(BaseTool):
    """A tool to stage artifacts from the artifact service to the local filesystem.

    This tool enables working with user-uploaded files by staging them from the
    artifact store to a local working directory where they can be accessed by
    scripts, commands, and other tools.

    Workflow:
    1. Stage: Copy artifacts from artifact store to local 'uploads/' directory
    2. Access: Use the staged files in commands, scripts, or other processing
    """

    def __init__(self):
        super().__init__(
            name="stage_artifacts",
            description=(
                "Stage artifacts from the artifact store to a local filesystem path, "
                "making them available for processing by tools and scripts.\n\n"
                "WORKFLOW:\n"
                "1. When a user uploads a file, it's stored as an artifact with a name\n"
                "2. Use this tool to copy the artifact to your local 'uploads/' directory\n"
                "3. Then reference the staged file path in commands or scripts\n\n"
                "USAGE EXAMPLE:\n"
                "- stage_artifacts(artifact_names=['data.csv'])\n"
                "  Returns: 'Successfully staged 1 file(s): uploads/data.csv (1.2 MB)'\n"
                "- Then use: bash('python scripts/process.py uploads/data.csv')\n\n"
                "PARAMETERS:\n"
                "- artifact_names: List of artifact names to stage (required)\n"
                "- destination_path: Target directory within session (default: 'uploads/')\n\n"
                "BEST PRACTICES:\n"
                "- Always stage artifacts before using them\n"
                "- Use default 'uploads/' destination for consistency\n"
                "- Stage all artifacts at the start of your workflow\n"
                "- Check returned paths to confirm successful staging"
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration | None:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "artifact_names": types.Schema(
                        type=types.Type.ARRAY,
                        description=(
                            "List of artifact names to stage. These are artifact identifiers "
                            "provided by the system when files are uploaded. "
                            "The tool will copy each artifact from the artifact store to the destination directory."
                        ),
                        items=types.Schema(type=types.Type.STRING),
                    ),
                    "destination_path": types.Schema(
                        type=types.Type.STRING,
                        description=(
                            "Relative path within the session directory to save the files. "
                            "Default is 'uploads/' where user-uploaded files are conventionally stored. "
                            "Path must be within the session directory for security. "
                            "Useful for organizing different types of artifacts (e.g., 'uploads/input/', 'uploads/processed/')."
                        ),
                        default="uploads/",
                    ),
                },
                required=["artifact_names"],
            ),
        )

    @override
    async def run_async(self, *, args: dict[str, Any], tool_context: ToolContext) -> str:
        artifact_names: List[str] = args.get("artifact_names", [])
        destination_path_str: str = args.get("destination_path", "uploads/")

        if not tool_context._invocation_context.artifact_service:
            return "Error: Artifact service is not available in this context."

        try:
            staging_root = get_session_path(session_id=tool_context.session.id)
            destination_dir = (staging_root / destination_path_str).resolve()

            # Security: Ensure the destination is within the staging path
            if staging_root not in destination_dir.parents and destination_dir != staging_root:
                return f"Error: Invalid destination path '{destination_path_str}'."

            destination_dir.mkdir(parents=True, exist_ok=True)

            staged_files = []
            for name in artifact_names:
                artifact = await tool_context.load_artifact(name)
                if artifact is None or artifact.inline_data is None:
                    logger.warning('Artifact "%s" not found or has no data, skipping', name)
                    continue

                # Check file size
                data_size = len(artifact.inline_data.data)
                if data_size > MAX_ARTIFACT_SIZE_BYTES:
                    size_mb = data_size / (1024 * 1024)
                    logger.warning(f'Artifact "{name}" exceeds size limit: {size_mb:.1f} MB')
                    continue

                # Use artifact name as filename (frontend should provide meaningful names)
                # If name has no extension, try to infer from MIME type
                filename = self._ensure_proper_extension(name, artifact.inline_data.mime_type)
                output_file = destination_dir / filename

                # Write file to disk
                output_file.write_bytes(artifact.inline_data.data)

                relative_path = output_file.relative_to(staging_root)
                size_kb = data_size / 1024
                staged_files.append(f"{relative_path} ({size_kb:.1f} KB)")

                logger.info(f"Staged artifact: {name} -> {relative_path} ({size_kb:.1f} KB)")

            if not staged_files:
                return "No valid artifacts were staged."

            return f"Successfully staged {len(staged_files)} file(s):\n" + "\n".join(
                f"  • {file}" for file in staged_files
            )

        except Exception as e:
            logger.error("Error staging artifacts: %s", e, exc_info=True)
            return f"An error occurred while staging artifacts: {e}"

    def _ensure_proper_extension(self, filename: str, mime_type: str) -> str:
        """Ensure filename has proper extension based on MIME type.

        If filename already has an extension, keep it.
        If not, add extension based on MIME type.

        Args:
            filename: Original filename from artifact
            mime_type: MIME type of the file

        Returns:
            Filename with proper extension
        """
        if not filename or not mime_type:
            return filename

        # If filename already has an extension, use it
        if Path(filename).suffix:
            return filename

        # Try to infer extension from MIME type
        extension = mimetypes.guess_extension(mime_type)
        if extension:
            return f"{filename}{extension}"

        return filename
