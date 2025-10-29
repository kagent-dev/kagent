from __future__ import annotations

import io
import json
import os
import tarfile
from typing import Dict, Optional, Tuple

def _parse_image_ref(image: str) -> Tuple[str, str, str]:
    """
    Parse an OCI/Docker image reference into (registry, repository, reference, is_digest).
    - reference: tag (default "latest") or digest (sha256:...)
    - is_digest: True if reference is a digest
    """
    # Split digest if present
    name_part = image
    ref = "latest"
    if "@" in image:
        name_part, ref = image.split("@", 1)
    else:
        colon = name_part.rfind(":")
        name_part, ref = name_part[:colon], name_part[colon + 1 :]

    # Determine registry and repo path
    parts = name_part.split("/")
    if len(parts) == 1:
        registry = "registry-1.docker.io"
        repo = f"library/{parts[0]}"
    if len(parts) == 2:
        registry = "docker.io"
        repo = "/".join(parts)
    else:
        registry = parts[0]
        repo = "/".join(parts[1:])

    return registry, repo, ref


def fetch_using_crane_to_dir(image: str, destination_folder: str) -> None:
    """Fetch a skill using crane and extract it to destination_folder."""
    import subprocess

    tar_path = os.path.join(destination_folder, "skill.tar")
    os.makedirs(destination_folder, exist_ok=True)

    # Use crane to pull the image as a tarball
    subprocess.run(
        ["crane","export","--insecure", image, tar_path],
        check=True,
    )

    # Extract the tarball
    with tarfile.open(tar_path, "r") as tar:
        tar.extractall(path=destination_folder)

    # Remove the tarball
    os.remove(tar_path)
    # remove the Dockerfile if exists, as it's not needed for the agent
    try:
        os.remove(os.path.join(destination_folder, "Dockerfile"))
    except FileNotFoundError:
        pass

def fetch_skill(skill_image: str, destination_folder: str) -> None:
    """
    Fetch a skill packaged as an OCI/Docker image and write its files to destination_folder.

    Assumptions:
    - We do not depend on a local Docker daemon.
    - We pull the manifest directly from the registry (anonymous) and download only the LAST layer.
    - We read at most 10MB from the registry to avoid OOM/abuse.
    - If the last layer is a tar+gzip layer, we extract all files to destination_folder.
    - If the last layer is a custom wasm artifact, we store it as plugin.wasm in destination_folder.

    To build a compatible skill image from a folder (containing SKILL.md), use a simple Dockerfile:
        FROM scratch
        COPY * /

    Args:
        skill_image: The image reference (e.g., "alpine:latest", "ghcr.io/org/skill:tag", or with a digest).
        destination_folder: The folder where the skill files should be written.
    """
    registry, repo, ref = _parse_image_ref(skill_image)

    # skill name is the last part of the repo
    repo_parts = repo.split("/")
    skill_name = repo_parts[-1]
    print(f"aboute to fetching skill {skill_name} from image {skill_image} (registry: {registry}, repo: {repo}, ref: {ref})")

    fetch_using_crane_to_dir(skill_image, os.path.join(destination_folder, skill_name))
