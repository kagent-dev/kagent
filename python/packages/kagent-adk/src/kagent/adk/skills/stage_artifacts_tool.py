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

"""A tool for staging artifacts from the artifact service to a local filesystem path."""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, List
import os
import tempfile

from google.genai import types
from typing_extensions import override

from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext

logger = logging.getLogger("google_adk." + __name__)


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
            os.symlink(
                skills_directory.resolve(),
                skills_symlink,
                target_is_directory=True,
            )

    return session_path.resolve()


class StageArtifactsTool(BaseTool):
    """A tool to stage artifacts from the artifact service to the local filesystem."""

    def __init__(self, skills_directory: Path):
        super().__init__(
            name="stage_artifacts",
            description=(
                "Copies artifacts from the artifact store to a local filesystem path, "
                "making them available for file-based tools like the shell."
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
                        description="A list of artifact names to stage.",
                        items=types.Schema(type=types.Type.STRING),
                    ),
                    "destination_path": types.Schema(
                        type=types.Type.STRING,
                        description="The local directory path to save the files to. Defaults to 'uploads/'.",
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
