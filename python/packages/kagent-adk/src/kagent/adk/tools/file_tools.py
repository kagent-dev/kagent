"""File operation tools for agent skills.

This module provides Read, Write, and Edit tools that agents can use to work with
files on the filesystem within the sandbox environment.
"""

from __future__ import annotations

import asyncio
import concurrent.futures
import functools
import logging
import multiprocessing
from collections.abc import Callable
from pathlib import Path
from typing import Any, Dict

from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from kagent.skills import (
    edit_file_content,
    get_edit_file_description,
    get_grep_file_description,
    get_list_files_description,
    get_read_file_description,
    get_session_path,
    get_write_file_description,
    grep_content,
    list_dir_content,
    read_file_content,
    write_file_content,
)

logger = logging.getLogger("kagent_adk." + __name__)


def _kill_workers(executor: concurrent.futures.ProcessPoolExecutor) -> None:
    """Terminate the pool's worker processes.

    ProcessPoolExecutor has no public way to stop a worker that is mid-call:
    shutdown() waits for it rather than ending it, and cancelling the future
    does nothing to code that never checks for cancellation. So a runaway
    match has to be killed through the private _processes mapping.

    If a future Python removes that attribute this must be *loud*. Silently
    skipping the kill would restore the original defect -- a pathological
    pattern spinning a core until the pod restarts -- with no signal at all.
    """
    processes = getattr(executor, "_processes", None)
    if processes is None:
        logger.warning(
            "ProcessPoolExecutor._processes is unavailable; a timed-out grep "
            "worker cannot be killed and may spin until the process exits"
        )
        return
    for proc in list(processes.values()):
        try:
            proc.kill()
        except OSError:
            # ProcessLookupError when the worker already exited -- expected
            # on the success path. Deliberately narrow: an AttributeError
            # here would mean kill() changed shape, and that must surface
            # rather than be swallowed, same as a missing _processes above.
            logger.debug("grep_file: worker already gone", exc_info=True)


async def _run_with_deadline(call: Callable[[], str], timeout_seconds: float) -> str:
    """Run call in a worker process, abandoning and killing it past the deadline.

    A process rather than a thread, and that is load-bearing rather than a
    tuning choice. `re` does not release the GIL while matching, so a
    catastrophic pattern run in a *thread* freezes every thread in the
    interpreter -- the event loop included. asyncio.wait_for then cannot fire,
    because the coroutine that would fire it never gets scheduled: the timeout
    silently becomes unenforceable exactly when it is needed. Reproduced with
    `(a+)+$` against 32 "a"s plus "!": a 0.5s timeout did not fire and an
    independent 50ms heartbeat stopped dead until the process was killed.

    A worker process has its own GIL, so the loop stays responsive, the
    deadline is honored, and -- unlike a thread -- the runaway can be killed.

    The pool is per-call and single-worker. A shared pool would save ~100ms of
    spawn per call, but then a timeout has to recycle shared state: killing
    workers belonging to unrelated in-flight greps and surfacing
    BrokenProcessPool to callers that did nothing wrong. At ~100ms on a tool
    call inside a multi-second model turn, that complexity is not worth
    buying; one call owning one process keeps the failure story trivial.

    "spawn" rather than the Linux default "fork": this server is threaded, and
    forking a threaded process can deadlock a child that inherits a lock held
    by a thread that does not exist in it. Callers pass a callable defined in
    kagent.skills, so the spawned child imports only that light module, never
    this one's google.adk dependency chain.
    """
    loop = asyncio.get_running_loop()
    executor = concurrent.futures.ProcessPoolExecutor(
        max_workers=1,
        mp_context=multiprocessing.get_context("spawn"),
    )
    try:
        return await asyncio.wait_for(loop.run_in_executor(executor, call), timeout=timeout_seconds)
    finally:
        # Unconditional, and not only for the timeout path: a pool worker stays
        # alive after finishing a task, so on success there is still an idle
        # process here. Killing it is how a single-use pool is reclaimed --
        # shutdown() below signals the worker but does not end one that is
        # mid-match, which is the case that matters.
        _kill_workers(executor)
        executor.shutdown(wait=False, cancel_futures=True)


def _resolve_working_path(tool_context: ToolContext, path_str: str) -> tuple[Path, Path]:
    """Resolve path_str relative to the session's working directory.

    Returns (resolved_path, working_dir); callers use working_dir to build
    their allowed_root argument.
    """
    working_dir = get_session_path(session_id=tool_context.session.id)
    path = Path(path_str)
    if not path.is_absolute():
        path = working_dir / path
    return path.resolve(), working_dir


class ReadFileTool(BaseTool):
    """Read files with line numbers for precise editing."""

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="read_file",
            description=get_read_file_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to read (absolute or relative to working directory)",
                    ),
                    "offset": types.Schema(
                        type=types.Type.INTEGER,
                        description="Optional line number to start reading from (1-indexed)",
                    ),
                    "limit": types.Schema(
                        type=types.Type.INTEGER,
                        description="Optional number of lines to read",
                    ),
                },
                required=["file_path"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Read a file with line numbers."""
        file_path_str = args.get("file_path", "").strip()
        offset = args.get("offset")
        limit = args.get("limit")

        if not file_path_str:
            return "Error: No file path provided"

        try:
            path, working_dir = _resolve_working_path(tool_context, file_path_str)

            return read_file_content(path, offset, limit, allowed_root=[working_dir, Path(self.skills_directory)])
        except (FileNotFoundError, IsADirectoryError, PermissionError, IOError) as e:
            return f"Error reading file {file_path_str}: {e}"


class WriteFileTool(BaseTool):
    """Write content to files (overwrites existing files)."""

    def __init__(self):
        super().__init__(
            name="write_file",
            description=get_write_file_description(),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to write (absolute or relative to working directory)",
                    ),
                    "content": types.Schema(
                        type=types.Type.STRING,
                        description="Content to write to the file",
                    ),
                },
                required=["file_path", "content"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Write content to a file."""
        file_path_str = args.get("file_path", "").strip()
        content = args.get("content", "")

        if not file_path_str:
            return "Error: No file path provided"

        try:
            path, working_dir = _resolve_working_path(tool_context, file_path_str)

            return write_file_content(path, content, allowed_root=working_dir)
        except (PermissionError, IOError) as e:
            error_msg = f"Error writing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg


class ListFilesTool(BaseTool):
    """List files and directories at a given path."""

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="list_files",
            description=get_list_files_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "path": types.Schema(
                        type=types.Type.STRING,
                        description="Directory path to list (absolute or relative to working directory); defaults to the working directory",
                    ),
                },
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """List the contents of a directory."""
        path_str = args.get("path", "").strip() or "."

        try:
            path, working_dir = _resolve_working_path(tool_context, path_str)

            return list_dir_content(path, allowed_root=[working_dir, Path(self.skills_directory)])
        except (FileNotFoundError, NotADirectoryError, PermissionError, IOError) as e:
            return f"Error listing {path_str}: {e}"


class GrepFileTool(BaseTool):
    """Search for a regular expression pattern in a file or directory."""

    # Bounds regex execution time: the pattern is agent-controlled, and Python's
    # backtracking `re` engine can take catastrophically long on adversarial
    # patterns (unlike Go's RE2-based regexp, which is linear-time). The match
    # runs in a worker process so this deadline can actually be enforced -- see
    # _run_with_deadline for why that is required rather than merely tidy.
    _TIMEOUT_SECONDS = 30

    def __init__(self, skills_directory: str | Path):
        super().__init__(
            name="grep_file",
            description=get_grep_file_description(),
        )
        self.skills_directory = Path(skills_directory).resolve()
        if not self.skills_directory.exists():
            raise ValueError(f"Skills directory does not exist: {self.skills_directory}")

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "pattern": types.Schema(
                        type=types.Type.STRING,
                        description="The regular expression pattern to search for",
                    ),
                    "path": types.Schema(
                        type=types.Type.STRING,
                        description="The file or directory path to search (absolute or relative to working directory)",
                    ),
                    "recursive": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Search directories recursively (default: false)",
                    ),
                    "ignore_case": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Ignore case when matching (default: false)",
                    ),
                },
                required=["pattern", "path"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Search a file or directory for a pattern."""
        # Deliberately not stripped: whitespace is meaningful in a regex, and
        # trimming turns "foo " into "foo", which then matches text the
        # caller's pattern excludes. Trim only to decide emptiness, and search
        # with what was actually asked for -- the same split skills.go makes.
        pattern = args.get("pattern", "")
        path_str = args.get("path", "").strip()
        recursive = args.get("recursive", False)
        ignore_case = args.get("ignore_case", False)

        if not pattern.strip():
            return "Error: No pattern provided"
        if not path_str:
            return "Error: No file path provided"

        try:
            path, working_dir = _resolve_working_path(tool_context, path_str)

            return await _run_with_deadline(
                functools.partial(
                    grep_content,
                    path,
                    pattern,
                    recursive=recursive,
                    ignore_case=ignore_case,
                    allowed_root=[working_dir, Path(self.skills_directory)],
                ),
                self._TIMEOUT_SECONDS,
            )
        except (TimeoutError, asyncio.TimeoutError):
            # asyncio.TimeoutError is TimeoutError on Python >=3.11, but this
            # package supports >=3.10 where they're distinct classes.
            return f"Error searching {path_str}: pattern took too long to match (possible catastrophic backtracking); try a simpler pattern"
        except (FileNotFoundError, IsADirectoryError, ValueError, PermissionError, IOError) as e:
            return f"Error searching {path_str}: {e}"


class EditFileTool(BaseTool):
    """Edit files by replacing exact string matches."""

    def __init__(self):
        super().__init__(
            name="edit_file",
            description=get_edit_file_description(),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "file_path": types.Schema(
                        type=types.Type.STRING,
                        description="Path to the file to edit (absolute or relative to working directory)",
                    ),
                    "old_string": types.Schema(
                        type=types.Type.STRING,
                        description="The exact text to replace (must exist in file)",
                    ),
                    "new_string": types.Schema(
                        type=types.Type.STRING,
                        description="The text to replace it with (must be different from old_string)",
                    ),
                    "replace_all": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="Replace all occurrences (default: false, only replaces first occurrence)",
                    ),
                },
                required=["file_path", "old_string", "new_string"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Edit a file by replacing old_string with new_string."""
        file_path_str = args.get("file_path", "").strip()
        old_string = args.get("old_string", "")
        new_string = args.get("new_string", "")
        replace_all = args.get("replace_all", False)

        if not file_path_str:
            return "Error: No file path provided"

        try:
            path, working_dir = _resolve_working_path(tool_context, file_path_str)

            return edit_file_content(path, old_string, new_string, replace_all, allowed_root=working_dir)
        except (FileNotFoundError, IsADirectoryError, ValueError, PermissionError, IOError) as e:
            error_msg = f"Error editing file {file_path_str}: {e}"
            logger.error(error_msg)
            return error_msg
