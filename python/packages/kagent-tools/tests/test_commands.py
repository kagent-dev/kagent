"""Command lifecycle and output contract shared with Go ADK."""

import asyncio
import contextlib
import ctypes
import os
import signal
import sys
from pathlib import Path

import pytest

from kagent.tools import execute_command, get_workspace_path
from kagent.tools.shell import _environment_contains, _parent_pid


@pytest.mark.asyncio
@pytest.mark.parametrize("exit_code", [0, 7])
async def test_diagnostics_are_retained(tmp_path, caplog, exit_code):
    result = await execute_command(
        f"printf 'private-output\\n'; printf 'WARNING: diagnostic\\n' >&2; exit {exit_code}", tmp_path
    )
    assert "private-output" in result
    assert "WARNING: diagnostic" in result
    assert "private-output" not in caplog.text
    if exit_code:
        assert "exit code 7" in result


@pytest.mark.asyncio
async def test_output_is_bounded_but_command_completes(tmp_path):
    result = await execute_command("head -c 1048576 /dev/zero; echo finished > finished", tmp_path)
    assert "[output truncated" in result
    assert len(result) < 66000
    assert (tmp_path / "finished").read_text() == "finished\n"


@pytest.mark.asyncio
async def test_cancellation_stops_descendants(tmp_path):
    task = asyncio.create_task(
        execute_command(
            "(echo started > started; while [ ! -f release ]; do sleep 0.01; done; echo leaked > leaked) & wait",
            tmp_path,
        )
    )
    try:

        async def started():
            while not (tmp_path / "started").exists():
                await asyncio.sleep(0.01)

        await asyncio.wait_for(started(), 3)
    finally:
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await asyncio.wait_for(task, 2)
    # A surviving child can only write after cancellation has finished.
    (tmp_path / "release").touch()
    await asyncio.sleep(0.6)
    assert not (tmp_path / "leaked").exists()


@pytest.mark.asyncio
async def test_timeout_stops_descendants_and_retains_output(tmp_path):
    result = await execute_command(
        "echo started; (sleep 1.5; echo leaked > leaked) & wait", tmp_path, timeout_seconds=1
    )
    assert "timed out after 1s" in result
    assert "started" in result
    await asyncio.sleep(0.6)
    assert not (tmp_path / "leaked").exists()


@pytest.mark.asyncio
async def test_background_pipe_does_not_hold_call_open(tmp_path):
    result = await asyncio.wait_for(execute_command("sleep 60 & echo done", tmp_path), 3)
    assert result == "done"


@pytest.mark.asyncio
async def test_venv_and_skill_environment(tmp_path):
    venv = tmp_path / "venv"
    (venv / "bin").mkdir(parents=True)
    executable = venv / "bin" / "kagent-test"
    executable.write_text("#!/bin/sh\nprintf 'venv\\n'")
    executable.chmod(0o755)
    result = await execute_command(
        'kagent-test; printf "%s\\n%s" "$VIRTUAL_ENV" "$PYTHONPATH"',
        tmp_path,
        skills_dir=Path("/skills"),
        bash_venv_path=str(venv),
    )
    assert "venv\n" + str(venv) in result
    assert str(tmp_path) + ":/skills" in result


@pytest.mark.asyncio
@pytest.mark.parametrize("seconds", [-1, 601])
async def test_timeout_bounds(tmp_path, seconds):
    result = await execute_command("echo should-not-run", tmp_path, timeout_seconds=seconds)
    assert "timeout_seconds must be between" in result


def test_workspace_reopens_existing_files(tmp_path, monkeypatch):
    monkeypatch.setattr("kagent.tools.workspace.WORKSPACE", tmp_path / "durable" / "workspace")
    workspace = get_workspace_path()
    (workspace / "notes.txt").write_text("saved")
    assert (get_workspace_path() / "notes.txt").read_text() == "saved"
    assert not (workspace / "uploads").exists()
    assert not (workspace / "outputs").exists()


@pytest.mark.asyncio
async def test_cancellation_during_startup_reaps_the_command(tmp_path, monkeypatch):
    actual_exec = asyncio.create_subprocess_exec
    spawned = asyncio.Event()
    release_handle = asyncio.Event()

    async def delayed_handle(*args, **kwargs):
        process = await actual_exec(*args, **kwargs)
        spawned.set()
        await release_handle.wait()
        return process

    monkeypatch.setattr(asyncio, "create_subprocess_exec", delayed_handle)
    task = asyncio.create_task(execute_command("sleep 60", tmp_path))
    try:
        await asyncio.wait_for(spawned.wait(), 3)
        task.cancel()
    finally:
        release_handle.set()
        with pytest.raises(asyncio.CancelledError):
            await asyncio.wait_for(task, 2)


@pytest.fixture
def child_subreaper():
    """Stand in for a runtime that is the sandbox's PID 1: orphans are reparented here."""
    prctl = ctypes.CDLL(None, use_errno=True).prctl
    pr_set_child_subreaper = 36
    assert prctl(pr_set_child_subreaper, 1, 0, 0, 0) == 0
    yield
    prctl(pr_set_child_subreaper, 0, 0, 0, 0)


@pytest.mark.skipif(sys.platform != "linux", reason="procfs and child subreapers are Linux only")
@pytest.mark.asyncio
@pytest.mark.parametrize(
    "job", ["sh -c 'echo $$ > pid; exec sleep 600' &", "setsid sh -c 'echo $$ > pid; exec sleep 600' &"]
)
async def test_command_stops_every_process_it_started(tmp_path, child_subreaper, job):
    # setsid leaves the command's process group, so the group kill alone misses it.
    result = await execute_command(f"{job} while [ ! -s pid ]; do sleep 0.01; done; echo started", tmp_path)
    assert result == "started"
    pid = int((tmp_path / "pid").read_text())
    try:
        # Not running, and not left as a zombie either.
        with pytest.raises(ProcessLookupError):
            os.kill(pid, 0)
    finally:
        with contextlib.suppress(ProcessLookupError):
            os.kill(pid, signal.SIGKILL)


def test_environment_contains():
    environ = b"PATH=/usr/bin\0KAGENT_BASH_CALL=7-12\0HOME=/root\0"
    assert _environment_contains(environ, "KAGENT_BASH_CALL=7-12")
    assert not _environment_contains(environ, "KAGENT_BASH_CALL=7-1")
    assert not _environment_contains(environ, "BASH_CALL=7-12")


def test_parent_pid():
    assert _parent_pid(b"4242 (sleep) S 1 4240 4240 0") == 1
    assert _parent_pid(b"4242 (a) b (c) R 77 4242 4242") == 77
    assert _parent_pid(b"4242 (truncated") is None
