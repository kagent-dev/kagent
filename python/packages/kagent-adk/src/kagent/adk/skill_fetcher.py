from __future__ import annotations

import os
import logging
import tarfile
from typing import Tuple

logger = logging.getLogger(__name__)


def _parse_image_ref(image: str) -> Tuple[str, str, str]:
    """
    Parse an OCI/Docker image reference into (registry, repository, reference).

    reference is either a tag (default "latest") or a digest (e.g., "sha256:...").
    Rules (compatible with Docker/OCI name parsing):
    - If the reference contains a digest ("@"), prefer a tag if also present (repo:tag@digest),
      otherwise keep the digest as the reference.
    - If there is no tag nor digest, default the reference to "latest".
    - If the first path component contains a '.' or ':' or equals 'localhost', it is treated as the registry.
      Otherwise the registry defaults to docker hub (docker.io), with the special library namespace for single-component names.
    """
    name_part = image
    ref = "latest"

    if "@" in image:
        # Split digest
        name_part, digest = image.split("@", 1)
        ref = digest

    # Possibly has a tag: detect a colon after the last slash
    slash = name_part.rfind("/")
    colon = name_part.rfind(":")
    if colon > slash:
        ref = name_part[colon + 1 :]
        name_part = name_part[:colon]
    # else: keep default "latest"

    # Determine registry and repo path
    parts = name_part.split("/")
    if len(parts) == 1:
        # Implicit docker hub library image
        registry = "registry-1.docker.io"
        repo = f"library/{parts[0]}"
    else:
        first = parts[0]
        if first == "localhost" or "." in first or ":" in first:
            # Explicit registry (may include port)
            registry = first
            repo = "/".join(parts[1:])
        else:
            # Docker hub with user/org namespace
            registry = "docker.io"
            repo = "/".join(parts)

    return registry, repo, ref


def fetch_using_crane_to_dir(image: str, destination_folder: str, insecure: bool = False) -> None:
    """Fetch a skill using crane and extract it to destination_folder."""
    import subprocess

    tar_path = os.path.join(destination_folder, "skill.tar")
    os.makedirs(destination_folder, exist_ok=True)
    command = ["crane", "export", image, tar_path]
    if insecure:
        command.insert(1, "--insecure")
    # Use crane to pull the image as a tarball
    subprocess.run(
        command,
        check=True,
    )

    # Extract the tarball
    with tarfile.open(tar_path, "r") as tar:
        tar.extractall(path=destination_folder, filter=tarfile.data_filter)

    # Remove the tarball
    os.remove(tar_path)


def fetch_skill(skill_image: str, destination_folder: str, insecure: bool = False) -> None:
    """
    Fetch a skill packaged as an OCI/Docker image and write its files to destination_folder.

    To build a compatible skill image from a folder (containing SKILL.md), use a simple Dockerfile:
        FROM scratch
        COPY . /

    Args:
        skill_image: The image reference (e.g., "alpine:latest", "ghcr.io/org/skill:tag", or with a digest).
        destination_folder: The folder where the skill files should be written.
    """
    registry, repo, ref = _parse_image_ref(skill_image)

    # skill name is the last part of the repo
    repo_parts = repo.split("/")
    skill_name = repo_parts[-1]
    logger.info(
        f"about to fetching skill {skill_name} from image {skill_image} (registry: {registry}, repo: {repo}, ref: {ref})"
    )

    fetch_using_crane_to_dir(skill_image, os.path.join(destination_folder, skill_name), insecure)
