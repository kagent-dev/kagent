"""Core, framework-agnostic logic for system tools (file and shell operations)."""

from __future__ import annotations

import asyncio
import enum
import logging
import os
import re
from pathlib import Path

logger = logging.getLogger(__name__)


# --- File Operation Tools ---

# Longest line read_file and grep_file will emit before truncating, counted in
# characters. Mirrors maxLineRunes in go/adk/pkg/tools/shell.go, and the
# "Lines longer than 2000 characters are truncated" both runtimes' read_file
# descriptions promise (see prompts.py).
_MAX_LINE_CHARS = 2000


def _truncate_line(line: str) -> str:
    """Shorten line to at most _MAX_LINE_CHARS characters, marking a cut with "..."."""
    if len(line) > _MAX_LINE_CHARS:
        return line[:_MAX_LINE_CHARS] + "..."
    return line


class _WalkEntryAction(enum.Enum):
    """How a recursive grep should treat one entry it walked."""

    #: A regular, in-bounds file that should be grepped.
    GREP = enum.auto()
    #: Silently excluded by policy, not a read failure -- a non-regular file
    #: (FIFO/socket/device), or a symlink whose target escapes the search root.
    SKIP = enum.auto()
    #: A genuine read/stat failure on this entry.
    UNREADABLE = enum.auto()


def _classify_walk_entry(root: Path, entry: Path) -> tuple[_WalkEntryAction, Path | None]:
    """Decide how grep_content should treat entry, given the resolved search root.

    This is the Python half of a contract the Go runtime implements as
    classifyWalkEntry in go/adk/pkg/tools/grep.go. Both must sort a tree into
    the same three outcomes, and reading them side by side is the only
    practical way to confirm they still do -- so they share these names, this
    argument order, and this return shape. Prefer changing both, or neither.

    They are not branch-for-branch identical, and should not be forced to be.
    Go tests IsDir, EvalSymlinks, Stat and IsRegular separately because
    filepath.WalkDir hands it directories and unresolvable links; here
    os.walk yields only filenames, and Path.is_file() already collapses
    "exists, resolves, and is a regular file" into one call. The outcomes
    agree; the number of branches reaching them does not.

    One known divergence, not reachable through any case we could construct:
    Go counts a failed Stat on a non-symlink as UNREADABLE, while
    Path.is_file() swallows the OSError and lands here on SKIP. Reaching it
    needs a stat that fails on an entry os.walk just listed -- EIO, a stale
    NFS handle, a misbehaving FUSE mount. Symlink loops and broken links are
    unaffected; both are UNREADABLE in either runtime.

    Returns the resolved path alongside GREP so the caller reads what was
    just checked rather than re-resolving the symlink separately.
    """
    # is_file() follows symlinks and checks S_ISREG, so a False covers two
    # cases that deserve different treatment.
    if not entry.is_file():
        # A broken symlink (or a symlink loop) is a genuine read failure.
        # Counting it is what stops a tree of dangling links from reporting a
        # confidently empty "no matches found".
        if entry.is_symlink() and not entry.exists():
            return _WalkEntryAction.UNREADABLE, None
        # A FIFO/socket/device is excluded by policy, not failure: opening one
        # can block indefinitely, and grep has no business reading it.
        return _WalkEntryAction.SKIP, None

    resolved = entry.resolve()
    # Bound each entry by the directory actually being searched. The caller's
    # allowed_root is the whole session plus the skills dir, so it alone would
    # let a symlink here pull in a file from a sibling directory nobody asked
    # to search. Containment against root subsumes it: root was itself
    # validated against allowed_root, so anything under root is inside a root.
    if not resolved.is_relative_to(root):
        return _WalkEntryAction.SKIP, None

    return _WalkEntryAction.GREP, resolved


def _validate_path(
    file_path: Path,
    allowed_roots: Path | list[Path] | None,
) -> Path:
    """Resolve the path and ensure it is within at least one allowed root directory."""
    resolved = file_path.resolve()
    if allowed_roots is None:
        return resolved

    roots = [allowed_roots] if isinstance(allowed_roots, Path) else allowed_roots
    for root in roots:
        if resolved.is_relative_to(root.resolve()):
            return resolved

    root_list = ", ".join(str(r.resolve()) for r in roots)
    raise PermissionError(f"Access denied: {resolved} is outside the allowed directories: {root_list}")


def read_file_content(
    file_path: Path,
    offset: int | None = None,
    limit: int | None = None,
    allowed_root: Path | list[Path] | None = None,
) -> str:
    """Reads a file with line numbers, raising errors on failure."""
    file_path = _validate_path(file_path, allowed_root)

    if not file_path.exists():
        raise FileNotFoundError(f"File not found: {file_path}")

    if not file_path.is_file():
        raise IsADirectoryError(f"Path is not a file: {file_path}")

    try:
        lines = file_path.read_text(encoding="utf-8").splitlines()
    except Exception as e:
        raise OSError(f"Error reading file {file_path}: {e}") from e

    start = (offset - 1) if offset and offset > 0 else 0
    end = (start + limit) if limit else len(lines)

    result_lines = []
    for i, line in enumerate(lines[start:end], start=start + 1):
        result_lines.append(f"{i:6d}|{_truncate_line(line)}")

    if not result_lines:
        return "File is empty."

    return "\n".join(result_lines)


def write_file_content(file_path: Path, content: str, allowed_root: Path | None = None) -> str:
    """Writes content to a file, creating parent directories if needed."""
    file_path = _validate_path(file_path, allowed_root)

    try:
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(content, encoding="utf-8")
        logger.info(f"Successfully wrote to {file_path}")
        return f"Successfully wrote to {file_path}"
    except Exception as e:
        raise OSError(f"Error writing file {file_path}: {e}") from e


def edit_file_content(
    file_path: Path,
    old_string: str,
    new_string: str,
    replace_all: bool = False,
    allowed_root: Path | None = None,
) -> str:
    """Performs an exact string replacement in a file."""
    if old_string == new_string:
        raise ValueError("old_string and new_string must be different")

    file_path = _validate_path(file_path, allowed_root)

    if not file_path.exists():
        raise FileNotFoundError(f"File not found: {file_path}")

    if not file_path.is_file():
        raise IsADirectoryError(f"Path is not a file: {file_path}")

    try:
        content = file_path.read_text(encoding="utf-8")
    except Exception as e:
        raise OSError(f"Error reading file {file_path}: {e}") from e

    if old_string not in content:
        raise ValueError(f"old_string not found in {file_path}")

    count = content.count(old_string)
    if not replace_all and count > 1:
        raise ValueError(
            f"old_string appears {count} times in {file_path}. Provide more context or set replace_all=true."
        )

    if replace_all:
        new_content = content.replace(old_string, new_string)
    else:
        new_content = content.replace(old_string, new_string, 1)

    try:
        file_path.write_text(new_content, encoding="utf-8")
        logger.info(f"Successfully replaced {count} occurrence(s) in {file_path}")
        return f"Successfully replaced {count} occurrence(s) in {file_path}"
    except Exception as e:
        raise OSError(f"Error writing file {file_path}: {e}") from e


def list_dir_content(dir_path: Path, allowed_root: Path | list[Path] | None = None) -> str:
    """Lists the entries of a directory, one per line.

    Directories are suffixed with "/"; files are followed by their size in bytes.
    """
    dir_path = _validate_path(dir_path, allowed_root)

    if not dir_path.exists():
        raise FileNotFoundError(f"Directory not found: {dir_path}")

    if not dir_path.is_dir():
        raise NotADirectoryError(f"Path is not a directory: {dir_path}")

    entries = sorted(dir_path.iterdir(), key=lambda p: p.name)
    if not entries:
        return "Directory is empty."

    lines = []
    for entry in entries:
        if entry.is_dir():
            lines.append(f"{entry.name}/")
            continue
        try:
            size = entry.stat().st_size
        except OSError:
            lines.append(entry.name)
            continue
        lines.append(f"{entry.name}\t{size}")

    return "\n".join(lines)


def grep_content(
    file_or_dir_path: Path,
    pattern: str,
    recursive: bool = False,
    ignore_case: bool = False,
    allowed_root: Path | list[Path] | None = None,
) -> str:
    """Searches path for lines matching a regular expression pattern.

    If path is a directory, recursive must be true to search its files.

    pattern is untrusted, agent-controlled input: Python's backtracking `re`
    engine can take catastrophically long on an adversarial pattern. This
    function does not bound its own execution time -- callers must do so
    (e.g. via a timeout around a thread/process offload) if the caller is
    exposed to untrusted patterns.
    """
    file_or_dir_path = _validate_path(file_or_dir_path, allowed_root)

    try:
        compiled = re.compile(pattern, re.IGNORECASE if ignore_case else 0)
    except re.error as e:
        raise ValueError(f"invalid pattern: {e}") from e

    if not file_or_dir_path.exists():
        raise FileNotFoundError(f"Path not found: {file_or_dir_path}")

    def grep_file(file_path: Path) -> list[str]:
        matches = []
        with file_path.open("r", encoding="utf-8", errors="replace") as f:
            for line_num, line in enumerate(f, start=1):
                line = line.rstrip("\n")
                if compiled.search(line):
                    matches.append(f"{file_path}:{line_num}:{_truncate_line(line)}")
        return matches

    results: list[str] = []
    skipped = 0
    if file_or_dir_path.is_dir():
        if not recursive:
            raise IsADirectoryError(f"{file_or_dir_path} is a directory; set recursive=true to search directories")

        # os.walk (not rglob) so that a directory-level failure -- root or
        # nested -- is observable via onerror. rglob() silently omits any
        # directory it can't list, at any depth, with no hook to detect it;
        # that let a nested unreadable subdirectory disappear from a
        # recursive search with no signal at all, exactly the "confidently
        # wrong empty result" failure mode skipped/the annotation below
        # exists to prevent. followlinks defaults to False, so this doesn't
        # descend into symlinked directories, matching grepFile's Go twin.
        root_str = str(file_or_dir_path)
        walk_errors: list[OSError] = []
        entries: list[Path] = []
        for dirpath, _dirnames, filenames in os.walk(file_or_dir_path, onerror=walk_errors.append):
            entries.extend(Path(dirpath) / name for name in filenames)

        for walk_err in walk_errors:
            if walk_err.filename == root_str:
                # The search root itself couldn't be read: the search never
                # actually ran, so surface a real error instead of a
                # misleadingly confident "no matches found".
                raise OSError(f"{file_or_dir_path} could not be read: {walk_err}") from walk_err
        # A nested subdirectory that couldn't be read shouldn't abort
        # matches already found in sibling directories -- just count it.
        skipped += len(walk_errors)

        for entry in sorted(entries):
            action, safe_entry = _classify_walk_entry(file_or_dir_path, entry)
            if action is _WalkEntryAction.SKIP:
                continue
            if action is _WalkEntryAction.UNREADABLE:
                skipped += 1
                continue
            try:
                results.extend(grep_file(safe_entry))
            except OSError:
                # A read error on one file shouldn't abort matches already
                # found elsewhere in the tree, but it also shouldn't look
                # identical to a genuinely empty search -- hence the count.
                skipped += 1
    else:
        if not file_or_dir_path.is_file():
            raise OSError(f"{file_or_dir_path} is not a regular file")
        results.extend(grep_file(file_or_dir_path))

    if not results:
        if skipped:
            return f"no matches found ({skipped} entries could not be read)"
        return "no matches found"

    output = "\n".join(results)
    if skipped:
        output += f"\n\n({skipped} entries could not be read)"
    return output


# --- Shell Operation Tools ---

# Matches env-var names containing secret-related segments as whole
# underscore-delimited tokens (e.g. OPENAI_API_KEY, DATABASE_PASSWORD)
# but not partial hits like TOKENIZERS_PARALLELISM.
_SECRET_PATTERNS = re.compile(
    r"(?:^|_)(API_KEY|ACCESS_KEY|SECRET|TOKEN|PASSWORD|CREDENTIALS?|PRIVATE_KEY)(?:_|$)",
    re.IGNORECASE,
)

# Explicit denylist of known secret env vars injected by the kagent controller
# (see go/core/pkg/env/providers.go). Belt-and-suspenders: the regex handles
# the general case, this set catches any known vars that the regex might miss.
_SECRET_ENV_NAMES: set[str] = {
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "AZURE_OPENAI_API_KEY",
    "AZURE_AD_TOKEN",
    "GOOGLE_API_KEY",
    "GOOGLE_APPLICATION_CREDENTIALS",
    "AWS_ACCESS_KEY_ID",
    "AWS_SECRET_ACCESS_KEY",
    "AWS_SESSION_TOKEN",
    "AWS_BEARER_TOKEN_BEDROCK",
}


def _sanitize_env(env: dict[str, str] | None = None) -> dict[str, str]:
    """Return a copy of the environment with secret variables removed."""
    source = env if env is not None else os.environ
    return {k: v for k, v in source.items() if k not in _SECRET_ENV_NAMES and not _SECRET_PATTERNS.search(k)}


_ENABLE_FILE_SEARCH_TOOLS_ENV = "KAGENT_ENABLE_FILE_SEARCH_TOOLS"


def file_search_tools_enabled() -> bool:
    """Whether the list_files/grep_file tools are enabled.

    Opt-in (disabled by default): they let an agent enumerate and search the
    filesystem under its session/skills roots without invoking a shell, so
    deployments that want to grant that visibility do so deliberately rather
    than having it enabled implicitly. Note this gate is theirs alone -- bash
    is always registered.
    """
    return os.environ.get(_ENABLE_FILE_SEARCH_TOOLS_ENV, "").strip().lower() in ("1", "t", "true")


def _get_command_timeout_seconds(command: str) -> float:
    """Determine appropriate timeout for a command."""
    if "python " in command or "python3 " in command:
        return 60.0  # 1 minute for python scripts
    else:
        return 30.0  # 30 seconds for other commands


async def execute_command(
    command: str,
    working_dir: Path,
    skills_dir: Path = Path("/skills"),
) -> str:
    """Execute a shell command inside the agent runtime."""
    timeout = _get_command_timeout_seconds(command)

    env = _sanitize_env()
    # Add skills directory and working directory to PYTHONPATH
    pythonpath_additions = [str(working_dir), str(skills_dir)]
    if "PYTHONPATH" in env:
        pythonpath_additions.append(env["PYTHONPATH"])
    env["PYTHONPATH"] = ":".join(pythonpath_additions)

    # If a separate venv for shell commands is specified, use its python and pip
    # Otherwise the system python/pip will be used for backward compatibility
    bash_venv_path = os.environ.get("BASH_VENV_PATH")
    if bash_venv_path:
        bash_venv_bin = os.path.join(bash_venv_path, "bin")
        # Prepend bash venv to PATH so its python and pip are used
        env["PATH"] = f"{bash_venv_bin}:{env.get('PATH', '')}"
        env["VIRTUAL_ENV"] = bash_venv_path

    try:
        process = await asyncio.create_subprocess_exec(
            "bash",
            "-c",
            command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=working_dir,
            env=env,
        )

        try:
            stdout, stderr = await asyncio.wait_for(process.communicate(), timeout=timeout)
        except TimeoutError:
            process.kill()
            await process.wait()
            return f"Error: Command timed out after {timeout}s"

        stdout_str = stdout.decode("utf-8", errors="replace") if stdout else ""
        stderr_str = stderr.decode("utf-8", errors="replace") if stderr else ""

        if process.returncode != 0:
            error_msg = f"Command failed with exit code {process.returncode}"
            if stderr_str:
                error_msg += f":\n{stderr_str}"
            elif stdout_str:
                error_msg += f":\n{stdout_str}"
            return error_msg

        output = stdout_str
        if stderr_str and "WARNING" not in stderr_str:
            output += f"\n{stderr_str}"

        logger.info(f"Command executed successfully: {output}")

        return output.strip() if output.strip() else "Command completed successfully."

    except Exception as e:
        logger.error(f"Error executing command: {e}")
        return f"Error: {e}"
