import os
import sys
from pathlib import Path

import pytest

# Ensure the package's src/ is on sys.path for "src" layout
_PKG_ROOT = Path(__file__).resolve().parents[2]  # .../packages/kagent-adk
_SRC = _PKG_ROOT / "src"
if str(_SRC) not in sys.path:
    sys.path.insert(0, str(_SRC))

from kagent.adk.skill_fetcher import _parse_image_ref  # noqa: E402


@pytest.mark.parametrize(
    "image,expected",
    [
        # Docker Hub implicit library and latest tag
        ("alpine", ("registry-1.docker.io", "library/alpine", "latest")),
        ("ubuntu", ("registry-1.docker.io", "library/ubuntu", "latest")),
        # Explicit tag on Docker Hub implicit library
        ("alpine:3.19", ("registry-1.docker.io", "library/alpine", "3.19")),
        # User namespace on Docker Hub
        ("user/image", ("docker.io", "user/image", "latest")),
        ("user/image:tag", ("docker.io", "user/image", "tag")),
        # Fully-qualified registry without tag -> default latest
        ("ghcr.io/org/skill", ("ghcr.io", "org/skill", "latest")),
        ("ghcr.io/org/skill:1.2.3", ("ghcr.io", "org/skill", "1.2.3")),
        # Digest reference
        (
            "ghcr.io/org/skill@sha256:abcdef",
            ("ghcr.io", "org/skill", "sha256:abcdef"),
        ),
        # Tag + digest present: keep the tag as ref (current behavior)
        (
            "ghcr.io/org/skill:1@sha256:abcdef",
            ("ghcr.io", "org/skill", "1"),
        ),
        # Registry with port
        (
            "registry.example.com:5000/repo/image:tag",
            ("registry.example.com:5000", "repo/image", "tag"),
        ),
        (
            "registry.example.com:5000/repo/image",
            ("registry.example.com:5000", "repo/image", "latest"),
        ),
        (
            "localhost:5000/image",
            ("localhost:5000", "image", "latest"),
        ),
    ],
)
def test_parse_image_ref(image, expected):
    assert _parse_image_ref(image) == expected
