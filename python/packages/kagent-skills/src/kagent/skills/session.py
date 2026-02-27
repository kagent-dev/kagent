"""Manages isolated filesystem paths for agent sessions."""

import logging
import tempfile
from pathlib import Path

logger = logging.getLogger(__name__)

# Cache of initialized session paths to avoid re-creating symlinks
_session_path_cache: dict[str, Path] = {}


def initialize_session_path(session_id: str, skills_directory: str) -> Path:
    """Initialize a session's working directory with skills symlink.

    Creates the directory structure and symlink to the skills directory.

    Directory structure:
        /tmp/kagent/{session_id}/
        ├── skills/     -> symlink to skills_directory (read-only shared skills)
        ├── uploads/    -> staged user files (temporary)
        └── outputs/    -> generated files for return

    Args:
        session_id: The unique ID of the current session.
        skills_directory: Path to the shared skills directory.

    Returns:
        The resolved path to the session's root directory.
    """
    # Return cached path if already initialized
    if session_id in _session_path_cache:
        return _session_path_cache[session_id]

    # Initialize new session path
    base_path = Path(tempfile.gettempdir()) / "kagent"
    session_path = base_path / session_id

    # Create working directories
    (session_path / "uploads").mkdir(parents=True, exist_ok=True)
    (session_path / "outputs").mkdir(parents=True, exist_ok=True)

    # Create symlink to skills directory
    skills_mount = Path(skills_directory)
    skills_link = session_path / "skills"
    if skills_mount.exists() and not skills_link.exists():
        try:
            skills_link.symlink_to(skills_mount)
            logger.debug(f"Created symlink: {skills_link} -> {skills_mount}")
        except FileExistsError:
            # Symlink already exists (race condition from concurrent session setup)
            pass
        except Exception as e:
            # Log but don't fail - skills can still be accessed via absolute path
            logger.warning(f"Failed to create skills symlink for session {session_id}: {e}")

    # Cache and return
    resolved_path = session_path.resolve()
    _session_path_cache[session_id] = resolved_path
    return resolved_path


def get_session_path(session_id: str) -> Path:
    """Get the working directory path for a session.

    This function retrieves the cached session path. If the session hasn't been
    initialized, it falls back to auto-initialization with default /skills directory.

    Args:
        session_id: The unique ID of the current session.

    Returns:
        The resolved path to the session's root directory.
    """
    # Return cached path if already initialized
    if session_id in _session_path_cache:
        return _session_path_cache[session_id]

    # Fallback: auto-initialize with default /skills
    logger.warning(
        f"Session {session_id} not initialized. "
        f"Auto-initializing with default /skills. "
        f"For custom skills directories, ensure the executor performs initialization."
    )
    return initialize_session_path(session_id, "/skills")


def clear_session_cache(session_id: str | None = None) -> None:
    """Clear cached session path(s).

    Args:
        session_id: Specific session to clear. If None, clears all cached sessions.
    """
    if session_id:
        _session_path_cache.pop(session_id, None)
    else:
        _session_path_cache.clear()
