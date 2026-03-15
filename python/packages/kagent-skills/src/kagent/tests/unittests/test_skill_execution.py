import asyncio
import json
import shutil
import tempfile
import textwrap
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from kagent.skills import (
    discover_skills,
    edit_file_content,
    execute_command,
    load_skill_content,
    read_file_content,
    write_file_content,
)
from kagent.skills.shell import _sanitize_env


@pytest.fixture
def skill_test_env() -> Path:
    """
    Creates a temporary environment that mimics a real session and ensures cleanup.

    This fixture manually creates and deletes the temporary directory structure
    to guarantee that no files are left behind after the test run.
    """
    # 1. Create a single top-level temporary directory
    top_level_dir = Path(tempfile.mkdtemp())

    try:
        session_dir = top_level_dir / "session"
        skills_root_dir = top_level_dir / "skills_root"

        # 2. Create session directories
        (session_dir / "uploads").mkdir(parents=True, exist_ok=True)
        (session_dir / "outputs").mkdir(parents=True, exist_ok=True)

        # 3. Create the skill to be tested
        skill_dir = skills_root_dir / "csv-to-json"
        script_dir = skill_dir / "scripts"
        script_dir.mkdir(parents=True, exist_ok=True)

        # SKILL.md
        (skill_dir / "SKILL.md").write_text(
            textwrap.dedent("""\
---
            name: csv-to-json
            description: Converts a CSV file to a JSON file.
            ---
            # CSV to JSON Conversion
            Use the `convert.py` script to convert a CSV file from the `uploads` directory
            to a JSON file in the `outputs` directory.
            Example: `bash("python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json")`
        """)
        )

        # Python script for the skill
        (script_dir / "convert.py").write_text(
            textwrap.dedent("""
            import csv
            import json
            import sys
            if len(sys.argv) != 3:
                print(f"Usage: python {sys.argv[0]} <input_csv> <output_json>")
                sys.exit(1)
            input_path, output_path = sys.argv[1], sys.argv[2]
            try:
                data = []
                with open(input_path, 'r', encoding='utf-8') as f:
                    reader = csv.DictReader(f)
                    for row in reader:
                        data.append(row)
                with open(output_path, 'w', encoding='utf-8') as f:
                    json.dump(data, f, indent=2)
                print(f"Successfully converted {input_path} to {output_path}")
            except FileNotFoundError:
                print(f"Error: Input file not found at {input_path}")
                sys.exit(1)
        """)
        )

        # 4. Create a symlink from the session to the skills root
        (session_dir / "skills").symlink_to(skills_root_dir, target_is_directory=True)

        # 5. Yield the session directory path to the test
        yield session_dir

    finally:
        # 6. Explicitly clean up the entire temporary directory
        shutil.rmtree(top_level_dir)


@pytest.mark.asyncio
async def test_skill_core_logic(skill_test_env: Path):
    """
    Tests the core logic of the 'csv-to-json' skill by directly
    calling the centralized tool functions.
    """
    session_dir = skill_test_env

    # 1. "Upload" a file for the skill to process
    input_csv_path = session_dir / "uploads" / "data.csv"
    input_csv_path.write_text("id,name\n1,Alice\n2,Bob\n")

    # 2. Execute the skill's core command, just as an agent would
    # We use the centralized `execute_command` function directly
    command = "python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json"
    result = await execute_command(command, working_dir=session_dir, skills_dir=Path("/skills"))

    assert "Successfully converted" in result

    # 3. Verify the output by reading the generated file
    # We use the centralized `read_file_content` function directly
    output_json_path = session_dir / "outputs" / "result.json"

    # The read_file_content function returns a string with line numbers,
    # so we need to parse it.
    raw_output = read_file_content(output_json_path)
    json_content_str = "\n".join(line.split("|", 1)[1] for line in raw_output.splitlines())

    # Assert the content is correct
    expected_data = [{"id": "1", "name": "Alice"}, {"id": "2", "name": "Bob"}]
    assert json.loads(json_content_str) == expected_data


@pytest.mark.asyncio
async def test_execute_command_no_shell_injection(tmp_path):
    """
    Verifies that shell metacharacters in the command are not interpreted by an
    outer shell. The command must be passed as a single argument to srt, not
    parsed by /bin/sh, so injection payloads cannot escape the sandbox.
    """
    captured = {}

    async def mock_exec(*args, **kwargs):
        captured["args"] = args
        mock_process = MagicMock()
        mock_process.communicate = AsyncMock(return_value=(b"ok", b""))
        mock_process.returncode = 0
        return mock_process

    injection_payload = 'ls"; cat /etc/passwd; echo "pwned'

    with (
        patch("asyncio.create_subprocess_shell") as mock_shell,
        patch("asyncio.create_subprocess_exec", side_effect=mock_exec),
    ):
        await execute_command(injection_payload, working_dir=tmp_path, skills_dir=Path("/skills"))

    # Invariant 1: create_subprocess_shell must never be used.
    assert not mock_shell.called

    # Invariant 2: The entire payload must arrive as a single argument to srt, never split by a shell.
    args = captured["args"]
    # The first argument should still be the sandbox runner.
    assert args[0] == "srt"
    # The injection payload must appear exactly once as its own argument.
    assert injection_payload in args
    assert list(args).count(injection_payload) == 1


# --- Path traversal tests ---


def test_read_file_blocks_path_traversal(tmp_path):
    """Reading a file outside the allowed root must raise PermissionError."""
    outside_file = tmp_path.parent / "outside.txt"
    outside_file.write_text("secret")

    try:
        with pytest.raises(PermissionError, match="outside the allowed director"):
            read_file_content(outside_file, allowed_root=tmp_path)
    finally:
        outside_file.unlink(missing_ok=True)


def test_read_file_blocks_relative_traversal(tmp_path):
    """Relative paths like ../foo that escape the root must be blocked."""
    (tmp_path / "subdir").mkdir()
    outside = tmp_path.parent / "secret.txt"
    outside.write_text("secret")

    try:
        with pytest.raises(PermissionError, match="outside the allowed director"):
            read_file_content(
                tmp_path / "subdir" / "../../secret.txt",
                allowed_root=tmp_path,
            )
    finally:
        outside.unlink(missing_ok=True)


def test_read_file_allows_path_inside_root(tmp_path):
    """Files inside the allowed root should work normally."""
    f = tmp_path / "hello.txt"
    f.write_text("hello world")
    result = read_file_content(f, allowed_root=tmp_path)
    assert "hello world" in result


def test_read_file_allows_multiple_roots(tmp_path):
    """Read should succeed when the file is inside any of the allowed roots."""
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    skill_file = skills_dir / "script.py"
    skill_file.write_text("print('hello')")

    session_dir = tmp_path / "session"
    session_dir.mkdir()

    # File is in skills_dir, not session_dir — should still be allowed
    result = read_file_content(skill_file, allowed_root=[session_dir, skills_dir])
    assert "print('hello')" in result

    # File outside both roots should be blocked
    outside = tmp_path / "outside.txt"
    outside.write_text("secret")
    with pytest.raises(PermissionError, match="outside the allowed directories"):
        read_file_content(outside, allowed_root=[session_dir, skills_dir])


def test_write_file_blocks_path_traversal(tmp_path):
    """Writing a file outside the allowed root must raise PermissionError."""
    outside_path = tmp_path.parent / "evil.txt"
    with pytest.raises(PermissionError, match="outside the allowed director"):
        write_file_content(outside_path, "malicious", allowed_root=tmp_path)
    assert not outside_path.exists()


def test_edit_file_blocks_path_traversal(tmp_path):
    """Editing a file outside the allowed root must raise PermissionError."""
    outside_file = tmp_path.parent / "target.txt"
    outside_file.write_text("original")

    try:
        with pytest.raises(PermissionError, match="outside the allowed director"):
            edit_file_content(outside_file, "original", "hacked", allowed_root=tmp_path)
        # File must not have been modified
        assert outside_file.read_text() == "original"
    finally:
        outside_file.unlink(missing_ok=True)


def test_skill_discovery_and_loading(skill_test_env: Path):
    """
    Tests the core logic of discovering a skill and loading its instructions.
    """
    # The fixture creates the session dir, the skills are one level up in a separate dir
    skills_root_dir = skill_test_env.parent / "skills_root"

    # 1. Test skill discovery
    discovered = discover_skills(skills_root_dir)
    assert len(discovered) == 1
    skill_meta = discovered[0]
    assert skill_meta.name == "csv-to-json"
    assert "Converts a CSV file" in skill_meta.description

    # 2. Test skill content loading
    skill_content = load_skill_content(skills_root_dir, "csv-to-json")
    assert "name: csv-to-json" in skill_content
    assert "# CSV to JSON Conversion" in skill_content
    assert 'Example: `bash("python skills/csv-to-json/scripts/convert.py' in skill_content


def test_sanitize_env_strips_secrets():
    """Verify _sanitize_env removes env vars matching secret patterns."""
    secret_vars = {
        # Regex-matched vars
        "OPENAI_API_KEY": "sk-secret",
        "AZURE_OPENAI_API_KEY": "az-secret",
        "ANTHROPIC_API_KEY": "ant-secret",
        "GOOGLE_API_KEY": "goog-secret",
        "SERPER_API_KEY": "serp-secret",
        "LANGSMITH_API_KEY": "ls-secret",
        "GRAFANA_SERVICE_ACCOUNT_TOKEN": "graf-token",
        "DATABASE_PASSWORD": "db-pass",
        "MY_SECRET": "shh",
        "AWS_SECRET_ACCESS_KEY": "aws-secret",
        "SSH_PRIVATE_KEY": "key-data",
        "GIT_CREDENTIAL": "cred",
        "GIT_CREDENTIALS": "cred-plural",
        # Vars from providers.go that previously leaked
        "GOOGLE_APPLICATION_CREDENTIALS": "/path/to/sa.json",
        "AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
        "AZURE_AD_TOKEN": "az-ad-token",
        "AWS_SESSION_TOKEN": "session-tok",
        "AWS_BEARER_TOKEN_BEDROCK": "bearer-tok",
    }
    safe_vars = {
        "PATH": "/usr/bin",
        "HOME": "/home/user",
        "PYTHONPATH": "/some/path",
        "LANG": "en_US.UTF-8",
        "TOKENIZERS_PARALLELISM": "true",
        "GOOGLE_CLOUD_PROJECT": "my-project",
        "AWS_REGION": "us-east-1",
    }

    result = _sanitize_env({**secret_vars, **safe_vars})

    for key in secret_vars:
        assert key not in result, f"{key} should have been stripped"

    for key, value in safe_vars.items():
        assert result[key] == value, f"{key} should be preserved"


@pytest.mark.asyncio
async def test_execute_command_strips_secret_env_vars(tmp_path):
    """Secret env vars must not be passed to sandboxed subprocesses."""
    captured = {}

    async def mock_exec(*args, **kwargs):
        captured["env"] = kwargs.get("env", {})
        mock_process = MagicMock()
        mock_process.communicate = AsyncMock(return_value=(b"ok", b""))
        mock_process.returncode = 0
        return mock_process

    env_overrides = {
        "OPENAI_API_KEY": "sk-secret",
        "ANTHROPIC_API_KEY": "ant-secret",
        "GOOGLE_APPLICATION_CREDENTIALS": "/path/to/sa.json",
        "AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
        "PATH": "/usr/bin",
        "HOME": "/home/user",
    }

    with (
        patch.dict("os.environ", env_overrides, clear=True),
        patch("asyncio.create_subprocess_exec", side_effect=mock_exec),
    ):
        await execute_command("echo hello", working_dir=tmp_path)

    env = captured["env"]
    assert "OPENAI_API_KEY" not in env
    assert "ANTHROPIC_API_KEY" not in env
    assert "GOOGLE_APPLICATION_CREDENTIALS" not in env
    assert "AWS_ACCESS_KEY_ID" not in env
    assert env["HOME"] == "/home/user"
