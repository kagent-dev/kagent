"""The durable workspace owned by one Substrate Actor."""

from pathlib import Path

WORKSPACE = Path("/data/workspace")


def get_workspace_path() -> Path:
    """Prepare durable files shared by this Actor's root agent and subagents."""
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    return WORKSPACE.resolve()
