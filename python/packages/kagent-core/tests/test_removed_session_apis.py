"""Guard against removed Kagent-owned Python session APIs."""

from pathlib import Path

import pytest

PYTHON_ROOT = Path(__file__).parents[3]
SOURCE_SUFFIXES = {".py", ".md", ".json", ".yaml", ".yml"}
REMOVED_API_MARKERS = ("KAgent" + "Session", "_session" + "_service")


@pytest.mark.parametrize("marker", REMOVED_API_MARKERS)
def test_python_sources_do_not_reference_removed_kagent_session_apis(marker: str):
    matches = []
    for directory in (PYTHON_ROOT / "packages", PYTHON_ROOT / "samples"):
        for path in directory.rglob("*"):
            if path.suffix not in SOURCE_SUFFIXES or ".venv" in path.parts:
                continue
            if marker in path.read_text(encoding="utf-8"):
                matches.append(path.relative_to(PYTHON_ROOT))

    assert not matches, f"Removed Kagent session API {marker!r} found in: {matches}"
