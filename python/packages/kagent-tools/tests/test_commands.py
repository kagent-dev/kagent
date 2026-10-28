"""Command lifecycle and output contract shared with Go ADK."""

import asyncio
from pathlib import Path

import pytest

from kagent.tools import execute_command, get_workspace_path


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
