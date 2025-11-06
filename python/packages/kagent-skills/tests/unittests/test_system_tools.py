from pathlib import Path

import pytest

from kagent.skills import execute_command

# Mark all tests in this file as asyncio
pytestmark = pytest.mark.asyncio


async def test_execute_command_success(tmp_path: Path):
    """Tests successful execution of a simple shell command."""
    command = "echo 'Hello from bash'"
    result = await execute_command(command, working_dir=tmp_path)

    # When using SRT the output may include additional lines,
    # so we check for the expected output substring.
    assert "Hello from bash" in result


async def test_execute_command_error(tmp_path: Path):
    """Tests execution of a command that results in an error."""
    command = "ls /nonexistent_directory_that_does_not_exist"
    result = await execute_command(command, working_dir=tmp_path)

    assert "Command failed with exit code" in result


async def test_execute_command_chaining(tmp_path: Path):
    """Tests chaining commands with &&."""
    command = "echo 'first' && echo 'second'"
    result = await execute_command(command, working_dir=tmp_path)

    assert "first\nsecond" in result
