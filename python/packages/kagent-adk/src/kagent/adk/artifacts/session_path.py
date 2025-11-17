import logging
import tempfile
from pathlib import Path

logger = logging.getLogger("kagent_adk." + __name__)

# Cache of initialized session paths to avoid re-creating symlinks
_session_path_cache: dict[str, Path] = {}


def initialize_session_path(session_id: str, skills_directory: str) -> Path:
    """Initialize a session's working directory with skills symlink.

    This is called by SkillsPlugin.before_agent_callback() to ensure the session
    is set up before any tools run. Creates the directory structure and symlink
    to the skills directory.

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

    This function retrieves the cached session path that was initialized by
    SkillsPlugin. If the session hasn't been initialized (plugin not used),
    it falls back to auto-initialization with default /skills directory.

    Tools should call this function to get their working directory. The session
    must be initialized by SkillsPlugin before tools run, which happens automatically
    via the before_agent_callback() hook.

    Args:
        session_id: The unique ID of the current session.

    Returns:
        The resolved path to the session's root directory.

    Note:
        If session is not initialized, automatically initializes with /skills.
        For custom skills directories, ensure SkillsPlugin is installed.
    """
    # Return cached path if already initialized
    if session_id in _session_path_cache:
        return _session_path_cache[session_id]

    # Fallback: auto-initialize with default /skills
    logger.warning(
        f"Session {session_id} not initialized by SkillsPlugin. "
        f"Auto-initializing with default /skills. "
        f"Install SkillsPlugin for custom skills directories."
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
