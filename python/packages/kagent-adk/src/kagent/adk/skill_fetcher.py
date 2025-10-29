from __future__ import annotations

import io
import json
import os
import tarfile
from typing import Dict, Optional, Tuple

import httpx


# Media types we care about (aligned with imagefetcher.go)
MT_DOCKER_LAYER = "application/vnd.docker.image.rootfs.diff.tar.gzip"
MT_OCI_LAYER = "application/vnd.oci.image.layer.v1.tar+gzip"
MT_OCI_MANIFEST = "application/vnd.oci.image.manifest.v1+json"
MT_DOCKER_MANIFEST = "application/vnd.docker.distribution.manifest.v2+json"
MT_WASM_ARTIFACT = "application/vnd.module.wasm.content.layer.v1+wasm"


MAX_DOWNLOAD_BYTES = 10 * 1024 * 1024  # 10MB hard cap on data we read


def _is_registry_host(s: str) -> bool:
    return "." in s or ":" in s or s == "localhost"


def _normalize_registry_host(host: str) -> str:
    # Map docker hub hostnames to the v2 endpoint host
    if host in {"docker.io", "index.docker.io"}:
        return "registry-1.docker.io"
    return host


def _parse_image_ref(image: str) -> Tuple[str, str, str, bool]:
    """
    Parse an OCI/Docker image reference into (registry, repository, reference, is_digest).
    - reference: tag (default "latest") or digest (sha256:...)
    - is_digest: True if reference is a digest
    """
    # Split digest if present
    is_digest = False
    name_part = image
    ref = "latest"
    if "@" in image:
        name_part, ref = image.split("@", 1)
        is_digest = True
    else:
        # Split tag vs registry port: use last ':' after last '/'
        slash = name_part.rfind("/")
        colon = name_part.rfind(":")
        if colon > slash:
            name_part, ref = name_part[:colon], name_part[colon + 1 :]

    # Determine registry and repo path
    parts = name_part.split("/")
    if len(parts) == 1:
        registry = "registry-1.docker.io"
        repo = f"library/{parts[0]}"
    else:
        if _is_registry_host(parts[0]):
            registry = _normalize_registry_host(parts[0])
            repo = "/".join(parts[1:])
        else:
            registry = "registry-1.docker.io"
            repo = name_part
            if "/" not in repo:
                repo = f"library/{repo}"

    return registry, repo, ref, is_digest


def _parse_www_authenticate(header: str) -> Dict[str, str]:
    """Parse WWW-Authenticate: Bearer realm=...,service=...,scope=... into dict."""
    out: Dict[str, str] = {}
    if not header:
        return out
    # Expecting something like: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull"
    try:
        scheme, params = header.split(" ", 1)
        if scheme.lower() != "bearer":
            return out
        for item in params.split(","):
            if "=" in item:
                k, v = item.split("=", 1)
                v = v.strip().strip('"')
                out[k.strip()] = v
    except Exception:
        return {}
    return out


def _auth_token(client: httpx.Client, auth_params: Dict[str, str], repo: str) -> Optional[str]:
    realm = auth_params.get("realm")
    if not realm:
        return None
    params = {}
    if "service" in auth_params:
        params["service"] = auth_params["service"]
    # Minimal scope for pulling
    params["scope"] = f"repository:{repo}:pull"
    r = client.get(realm, params=params, timeout=30)
    if r.status_code == 200:
        try:
            return r.json().get("token") or r.json().get("access_token")
        except json.JSONDecodeError:
            return None
    return None


def _get_manifest(client: httpx.Client, registry: str, repo: str, ref: str) -> Tuple[dict, str, Optional[str]]:
    """Return (manifest_json, manifest_media_type, bearer_token)"""
    url = f"https://{registry}/v2/{repo}/manifests/{ref}"
    headers = {
        "Accept": ", ".join(
            [
                MT_OCI_MANIFEST,
                MT_DOCKER_MANIFEST,
                "application/vnd.oci.image.index.v1+json",
            ]
        )
    }
    r = client.get(url, headers=headers, timeout=30)
    if r.status_code == 401:
        auth = _parse_www_authenticate(r.headers.get("Www-Authenticate") or r.headers.get("WWW-Authenticate", ""))
        token = _auth_token(client, auth, repo)
        if token:
            headers["Authorization"] = f"Bearer {token}"
            r = client.get(url, headers=headers, timeout=30)
            if r.status_code == 200:
                return r.json(), r.headers.get("Content-Type", ""), token
            r.raise_for_status()
        r.raise_for_status()
    r.raise_for_status()
    return r.json(), r.headers.get("Content-Type", ""), None


def _download_blob(client: httpx.Client, registry: str, repo: str, digest: str, bearer: Optional[str]) -> bytes:
    url = f"https://{registry}/v2/{repo}/blobs/{digest}"
    headers = {}
    if bearer:
        headers["Authorization"] = f"Bearer {bearer}"
    with client.stream("GET", url, headers=headers, timeout=60) as r:
        if r.status_code == 401:
            # Try challenge flow from blob request too
            auth = _parse_www_authenticate(r.headers.get("Www-Authenticate") or r.headers.get("WWW-Authenticate", ""))
            token = _auth_token(client, auth, repo)
            if token:
                headers["Authorization"] = f"Bearer {token}"
                return _download_blob(client, registry, repo, digest, token)
            r.raise_for_status()
        r.raise_for_status()
        buf = io.BytesIO()
        total = 0
        for chunk in r.iter_bytes():
            if not chunk:
                continue
            total += len(chunk)
            if total > MAX_DOWNLOAD_BYTES:
                raise ValueError("download exceeds 10MB limit")
            buf.write(chunk)
        return buf.getvalue()


def _safe_extract_tar_gz(data: bytes, destination_folder: str) -> None:
    os.makedirs(destination_folder, exist_ok=True)
    # Open the tar.gz from memory
    fileobj = io.BytesIO(data)
    try:
        with tarfile.open(fileobj=fileobj, mode="r:gz") as tf:
            for member in tf:
                # Only extract regular files and directories
                if not (member.isfile() or member.isdir() or member.islnk() or member.issym()):
                    continue

                # Prevent path traversal
                member_path = os.path.normpath(member.name.lstrip("/"))
                dest_path = os.path.join(destination_folder, member_path)
                dest_path = os.path.normpath(dest_path)
                if not dest_path.startswith(os.path.abspath(destination_folder)):
                    raise ValueError(f"unsafe path in archive: {member.name}")

                if member.isdir():
                    os.makedirs(dest_path, exist_ok=True)
                    continue

                # Ensure parent dirs
                os.makedirs(os.path.dirname(dest_path), exist_ok=True)

                # Extract file contents
                src = tf.extractfile(member)
                if src is None:
                    continue
                with src:  # type: ignore
                    with open(dest_path, "wb") as out:
                        while True:
                            chunk = src.read(1024 * 64)
                            if not chunk:
                                break
                            out.write(chunk)
    except tarfile.ReadError as e:
        raise ValueError(f"invalid tar.gz layer: {e}")


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
    registry, repo, ref, _ = _parse_image_ref(skill_image)

    with httpx.Client(follow_redirects=True) as client:
        manifest, mt, bearer = _get_manifest(client, registry, repo, ref)

        # Reject image indexes (multi-arch); keep scope simple for now
        if manifest.get("mediaType", "") in {"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"}:
            raise ValueError("multi-arch image indexes are not supported; please provide a concrete image manifest")

        layers = manifest.get("layers", [])
        if not layers:
            raise ValueError("image has no layers")

        last_layer = layers[-1]
        layer_media_type = (last_layer.get("mediaType") or "").lower()
        digest = last_layer.get("digest")
        if not digest:
            raise ValueError("last layer missing digest")

        blob = _download_blob(client, registry, repo, digest, bearer)

        # Handle different content types inspired by imagefetcher.go
        if layer_media_type in {MT_DOCKER_LAYER, MT_OCI_LAYER}:
            _safe_extract_tar_gz(blob, destination_folder)
            return

        if layer_media_type == MT_WASM_ARTIFACT:
            # Raw wasm bytes; write as plugin.wasm
            os.makedirs(destination_folder, exist_ok=True)
            out_path = os.path.join(destination_folder, "plugin.wasm")
            with open(out_path, "wb") as f:
                f.write(blob)
            return

        # As a fallback, try to treat the blob as tar.gz; if it fails, raise a clear error.
        try:
            _safe_extract_tar_gz(blob, destination_folder)
        except Exception as e:
            raise ValueError(
                f"unsupported layer media type '{layer_media_type}' and blob is not a valid tar.gz: {e}"
            )