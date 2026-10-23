"""Filesystem and command helpers for the Substrate sandbox."""

from __future__ import annotations

import asyncio
import enum
import os
import re
import signal
from pathlib import Path

_MAX_LINE_CHARS = 2000
_MAX_LINE_BYTES = 1024 * 1024
_MAX_FILE_OUTPUT = 64 * 1024
_DEFAULT_READ_LINES = 2000
_FILE_TRUNCATION_MARKER = "[output truncated; use read_file offset/limit or a narrower search]"


def _truncate_line(line: str) -> str:
    return line[:_MAX_LINE_CHARS] + "..." if len(line) > _MAX_LINE_CHARS else line


class _FileOutput:
    """Keep complete result lines within the same byte budget as Go."""

    def __init__(self):
        self.lines: list[str] = []
        self.size = 0
        self.truncated = False

    def append(self, line: str) -> bool:
        size = len(line.encode("utf-8")) + 1
        if self.size + size > _MAX_FILE_OUTPUT:
            self.truncated = True
            return False
        self.lines.append(line)
        self.size += size
        return True

    def text(self) -> str:
        lines = self.lines + ([_FILE_TRUNCATION_MARKER] if self.truncated else [])
        return "\n".join(lines)


def _file_lines(path: Path):
    if not path.is_file():
        raise OSError(f"{path} is not a regular file")
    with path.open("rb") as stream:
        line_num = 0
        while line := stream.readline(_MAX_LINE_BYTES + 1):
            if len(line) > _MAX_LINE_BYTES:
                raise OSError(f"Line exceeds {_MAX_LINE_BYTES} bytes in {path}")
            line_num += 1
            yield line_num, line.decode("utf-8", errors="replace").rstrip("\r\n")


class _WalkEntryAction(enum.Enum):
    GREP = enum.auto()
    SKIP = enum.auto()
    UNREADABLE = enum.auto()


def _classify_walk_entry(root: Path, entry: Path) -> tuple[_WalkEntryAction, Path | None]:
    """Skip special files and links outside the requested search tree."""
    try:
        if not entry.is_file():
            if entry.is_symlink() and not entry.exists():
                return _WalkEntryAction.UNREADABLE, None
            return _WalkEntryAction.SKIP, None
        resolved = entry.resolve()
    except OSError:
        return _WalkEntryAction.UNREADABLE, None
    if not resolved.is_relative_to(root):
        return _WalkEntryAction.SKIP, None
    return _WalkEntryAction.GREP, resolved


def read_file_content(file_path: Path, offset: int | None = None, limit: int | None = None) -> str:
    """Read a bounded page, without loading the whole file."""
    file_path = file_path.resolve()
    if not file_path.exists():
        raise FileNotFoundError(f"File not found: {file_path}")
    if not file_path.is_file():
        raise IsADirectoryError(f"Path is not a file: {file_path}")
    start = max(offset or 1, 1)
    line_limit = limit if limit and limit > 0 else _DEFAULT_READ_LINES
    result = _FileOutput()
    count = 0
    for line_num, line in _file_lines(file_path):
        if line_num < start:
            continue
        count += 1
        if not result.append(f"{line_num:6d}|{_truncate_line(line)}"):
            break
        if count == line_limit:
            if not limit or limit < 0:
                result.append(f"[read limited to {line_limit} lines; continue with offset={line_num + 1}]")
            break
    return result.text() or "File is empty."


def write_file_content(file_path: Path, content: str) -> str:
    file_path.parent.mkdir(parents=True, exist_ok=True)
    file_path.write_text(content, encoding="utf-8")
    return f"Successfully wrote to {file_path}"


def edit_file_content(file_path: Path, old_string: str, new_string: str, replace_all: bool = False) -> str:
    if not old_string:
        raise ValueError("old_string cannot be empty")
    if old_string == new_string:
        raise ValueError("old_string and new_string must be different")
    content = file_path.read_text(encoding="utf-8")
    count = content.count(old_string)
    if not count:
        raise ValueError(f"old_string not found in {file_path}")
    if not replace_all and count > 1:
        raise ValueError(
            f"old_string appears {count} times in {file_path}. Provide more context or set replace_all=true."
        )
    file_path.write_text(content.replace(old_string, new_string, -1 if replace_all else 1), encoding="utf-8")
    return f"Successfully replaced {count if replace_all else 1} occurrence(s) in {file_path}"


def list_dir_content(dir_path: Path) -> str:
    result = _FileOutput()
    with os.scandir(dir_path) as entries:
        for entry in entries:
            line = entry.name
            try:
                info = entry.stat()
                line += "/" if entry.is_dir() else f"\t{info.st_size}"
            except OSError:
                pass  # A broken or vanished entry is listed without metadata.
            if not result.append(line):
                break
    return result.text() or "Directory is empty."


def grep_content(file_or_dir_path: Path, pattern: str, recursive: bool = False, ignore_case: bool = False) -> str:
    """Stream bounded results. Callers bound Python regex execution with a worker timeout."""
    root = file_or_dir_path.resolve()
    try:
        compiled = re.compile(pattern, re.IGNORECASE if ignore_case else 0)
    except re.error as error:
        raise ValueError(f"invalid pattern: {error}") from error
    if not root.exists():
        raise FileNotFoundError(f"Path not found: {root}")
    result = _FileOutput()
    skipped = 0

    def grep_file(path: Path):
        for line_num, line in _file_lines(path):
            if compiled.search(line) and not result.append(f"{path}:{line_num}:{_truncate_line(line)}"):
                break

    def walk_error(error: OSError):
        nonlocal skipped
        if error.filename == str(root):
            raise OSError(f"{root} could not be read: {error}") from error
        skipped += 1

    if root.is_dir():
        if not recursive:
            raise IsADirectoryError(f"{root} is a directory; set recursive=true to search directories")
        for dirpath, _dirnames, filenames in os.walk(root, onerror=walk_error):
            for name in filenames:
                action, entry = _classify_walk_entry(root, Path(dirpath) / name)
                if action is _WalkEntryAction.UNREADABLE:
                    skipped += 1
                elif action is _WalkEntryAction.GREP:
                    try:
                        grep_file(entry)
                    except OSError:
                        skipped += 1
                if result.truncated:
                    break
            if result.truncated:
                break
    else:
        grep_file(root)
    if not result.lines and not result.truncated:
        return f"no matches found ({skipped} entries could not be read)" if skipped else "no matches found"
    output = result.text()
    return output + f"\n\n({skipped} entries could not be read)" if skipped else output


# --- Shell Operation Tools ---

_MAX_COMMAND_OUTPUT = 64 * 1024
_TRUNCATION_MARKER = "\n[output truncated at 65536 bytes; redirect output to a file for the full result]"


async def execute_command(
    command: str,
    working_dir: Path,
    skills_dir: Path | None = None,
    bash_venv_path: str = "",
    timeout_seconds: int = 0,
) -> str:
    """Run one command tree using the same defaults as the Go runtime."""
    if not isinstance(timeout_seconds, int) or not 0 <= timeout_seconds <= 600:
        return "Error: timeout_seconds must be between 0 and 600 (0 uses the 60-second default)"
    timeout = timeout_seconds or 60
    # Preserve the Actor environment, including SDK placeholders used by egress.
    env = dict(os.environ)
    python_paths = [str(working_dir)]
    if skills_dir is not None:
        python_paths.append(str(skills_dir))
    if env.get("PYTHONPATH"):
        python_paths.append(env["PYTHONPATH"])
    env["PYTHONPATH"] = os.pathsep.join(python_paths)
    if bash_venv_path:
        env["PATH"] = str(Path(bash_venv_path) / "bin") + os.pathsep + env.get("PATH", "")
        env["VIRTUAL_ENV"] = bash_venv_path
    startup = asyncio.create_task(
        asyncio.create_subprocess_exec(
            "bash",
            "-c",
            command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
            cwd=working_dir,
            env=env,
            start_new_session=True,
        )
    )
    captured = bytearray()
    truncated = False

    async def drain_output():
        nonlocal truncated
        while chunk := await process.stdout.read(8192):
            keep = min(len(chunk), _MAX_COMMAND_OUTPUT - len(captured))
            captured.extend(chunk[:keep])
            truncated = truncated or keep < len(chunk)

    async def wait_for_exit():
        # Process.wait() can wait for inherited pipes held by grandchildren.
        # Observe the shell exit independently, then stop its whole process group.
        while process.returncode is None:
            await asyncio.sleep(0.01)

    reader = None
    timed_out = False
    try:
        # Cancellation during process creation must not lose the process handle.
        process = await asyncio.shield(startup)
        reader = asyncio.create_task(drain_output())
        try:
            await asyncio.wait_for(wait_for_exit(), timeout)
        except TimeoutError:
            timed_out = True
    finally:
        process = await startup
        if reader is None:
            reader = asyncio.create_task(drain_output())
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass  # The entire group has already exited.
        try:
            await asyncio.wait_for(asyncio.shield(reader), 1)
        finally:
            if not reader.done():
                reader.cancel()
                await asyncio.gather(reader, return_exceptions=True)
            await asyncio.wait_for(process.wait(), 1)
    output = captured.decode("utf-8", errors="replace").strip()
    if truncated:
        output += _TRUNCATION_MARKER
    if timed_out:
        return f"Error: command timed out after {timeout}s\n{output}".rstrip()
    if process.returncode != 0:
        return f"Error: command failed with exit code {process.returncode}\n{output}".rstrip()
    return output or "Command completed successfully."
