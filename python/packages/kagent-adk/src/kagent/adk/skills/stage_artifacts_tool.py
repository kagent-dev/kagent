from __future__ import annotations

import logging
import os
import tempfile
from pathlib import Path
from typing import Any, List

from typing_extensions import override

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

logger = logging.getLogger("kagent_adk." + __name__)


def get_session_staging_path(session_id: str, app_name: str, skills_directory: Path) -> Path:
    """Creates (if needed) and returns the path to a session's staging directory.

    This function provides a consistent, isolated filesystem environment for each
    session. It creates a root directory for the session and populates it with
    an 'uploads' folder and a symlink to the static 'skills' directory.

    Args:
        session_id: The unique ID of the current session.
        app_name: The name of the application, used for namespacing.
        skills_directory: The path to the static skills directory.

    Returns:
        The resolved path to the session's root staging directory.
    """
    base_path = Path(tempfile.gettempdir()) / "adk_sessions" / app_name
    session_path = base_path / session_id

    # Create the session and uploads directories
    (session_path / "uploads").mkdir(parents=True, exist_ok=True)

    # Symlink the static skills directory into the session directory
    if skills_directory and skills_directory.exists():
        skills_symlink = session_path / "skills"
        if not skills_symlink.exists():
            try:
                os.symlink(
                    skills_directory.resolve(),
                    skills_symlink,
                    target_is_directory=True,
                )
            except OSError as e:
                logger.error(f"Failed to create skills symlink: {e}")

    return session_path.resolve()


class StageArtifactsTool(BaseTool):
    """A tool to stage artifacts from the artifact service to the local filesystem.

    This tool bridges the gap between the artifact store and the skills system,
    enabling skills to work with user-uploaded files through a two-phase workflow:
    1. Stage: Copy artifacts from artifact store to local 'uploads/' directory
    2. Execute: Use the staged files in bash commands with skills

    This is essential for the skills workflow where user-uploaded files must be
    accessible to skill scripts and commands.
    """

    def __init__(self, skills_directory: Path):
        super().__init__(
            name="stage_artifacts",
            description=(
                "Stage artifacts from the artifact store to a local filesystem path, "
                "making them available for use with skills and the bash tool.\n\n"
                "WORKFLOW:\n"
                "1. When a user uploads a file, it's stored as an artifact (e.g., 'artifact_xyz')\n"
                "2. Use this tool to copy the artifact to your local 'uploads/' directory\n"
                "3. Then reference the staged file path in bash commands\n\n"
                "USAGE EXAMPLE:\n"
                "- stage_artifacts(artifact_names=['artifact_xyz'])\n"
                "  Returns: 'Successfully staged 1 artifact(s) to: uploads/artifact_xyz'\n"
                "- Use the returned path in bash: bash('python skills/data-analysis/scripts/process.py uploads/artifact_xyz')\n\n"
                "PARAMETERS:\n"
                "- artifact_names: List of artifact names to stage (required)\n"
                "- destination_path: Target directory within session (default: 'uploads/')\n\n"
                "BEST PRACTICES:\n"
                "- Always stage artifacts before using them in skills\n"
                "- Use default 'uploads/' destination for consistency\n"
                "- Stage all artifacts at the start of your workflow\n"
                "- Check returned paths to confirm successful staging"
            ),
        )
        self._skills_directory = skills_directory

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
                            "provided by the system when files are uploaded (e.g., 'artifact_abc123'). "
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
            staging_root = get_session_staging_path(
                session_id=tool_context.session.id,
                app_name=tool_context._invocation_context.app_name,
                skills_directory=self._skills_directory,
            )
            destination_dir = (staging_root / destination_path_str).resolve()

            # Security: Ensure the destination is within the staging path
            if staging_root not in destination_dir.parents and destination_dir != staging_root:
                return f"Error: Invalid destination path '{destination_path_str}'."

            destination_dir.mkdir(parents=True, exist_ok=True)

            output_paths = []
            for name in artifact_names:
                artifact = await tool_context.load_artifact(name)
                if artifact is None or artifact.inline_data is None:
                    logger.warning('Artifact "%s" not found or has no data, skipping', name)
                    continue

                output_file = destination_dir / name
                output_file.write_bytes(artifact.inline_data.data)
                relative_path = output_file.relative_to(staging_root)
                output_paths.append(str(relative_path))

            if not output_paths:
                return "No valid artifacts were staged."

            return f"Successfully staged {len(output_paths)} artifact(s) to: {', '.join(output_paths)}"

        except Exception as e:
            logger.error("Error staging artifacts: %s", e, exc_info=True)
            return f"An error occurred while staging artifacts: {e}"
