import concurrent.futures
import json
import os
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
    grep_content,
    list_dir_content,
    load_skill_content,
    read_file_content,
    write_file_content,
)
from kagent.skills.shell import _get_srt_settings_args, _sanitize_env


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

    fake_bin_dir = session_dir.parent / "bin"
    fake_bin_dir.mkdir()
    fake_srt = fake_bin_dir / "srt"
    fake_srt.write_text('#!/bin/sh\nif [ "$1" = "--settings" ]; then\n  shift 2\nfi\nexec "$@"\n')
    fake_srt.chmod(0o755)

    settings_path = session_dir.parent / "srt-settings.json"
    settings_path.write_text(
        '{"network":{"allowedDomains":[],"deniedDomains":[]},"filesystem":{"denyRead":[],"allowWrite":[".","/tmp"],"denyWrite":[]}}'
    )

    # 2. Execute the skill's core command, just as an agent would
    # We use the centralized `execute_command` function directly
    command = "python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json"
    with patch.dict(
        "os.environ",
        {
            "KAGENT_SRT_SETTINGS_PATH": str(settings_path),
            "PATH": f"{fake_bin_dir}:{os.environ.get('PATH', '')}",
        },
        clear=False,
    ):
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
        patch.dict("os.environ", {"KAGENT_SRT_SETTINGS_PATH": "/config/srt-settings.json"}, clear=False),
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
    assert args[1] == "--settings"
    # The injection payload must appear exactly once as its own argument.
    assert injection_payload in args
    assert list(args).count(injection_payload) == 1


def test_get_srt_settings_args_uses_mounted_path():
    """Mounted srt settings should be used when the env var is present."""
    with patch.dict("os.environ", {"KAGENT_SRT_SETTINGS_PATH": "/config/srt-settings.json"}, clear=True):
        args = _get_srt_settings_args()

    assert args == ["--settings", "/config/srt-settings.json"]


def test_get_srt_settings_args_requires_mounted_path():
    """Sandbox execution should require the mounted settings path."""
    with patch.dict("os.environ", {}, clear=True):
        with pytest.raises(ValueError, match="KAGENT_SRT_SETTINGS_PATH is not set"):
            _get_srt_settings_args()


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


# --- list_dir_content / grep_content tests ---


def test_list_dir_content_lists_entries(tmp_path):
    (tmp_path / "b.txt").write_text("hello")
    (tmp_path / "a-subdir").mkdir()

    result = list_dir_content(tmp_path)
    assert "a-subdir/" in result
    assert "b.txt\t5" in result


def test_list_dir_content_empty_directory(tmp_path):
    assert list_dir_content(tmp_path) == "Directory is empty."


def test_list_dir_content_nonexistent_path(tmp_path):
    with pytest.raises(FileNotFoundError):
        list_dir_content(tmp_path / "does-not-exist")


def test_list_dir_content_blocks_path_traversal(tmp_path):
    outside = tmp_path.parent / "outside-dir"
    outside.mkdir(exist_ok=True)
    try:
        with pytest.raises(PermissionError, match="outside the allowed director"):
            list_dir_content(outside, allowed_root=tmp_path)
    finally:
        shutil.rmtree(outside, ignore_errors=True)


def test_grep_content_finds_match(tmp_path):
    f = tmp_path / "a.txt"
    f.write_text("hello world\nFOO bar\n")

    result = grep_content(f, "hello")
    assert "a.txt:1:hello world" in result


def test_grep_content_no_matches(tmp_path):
    f = tmp_path / "a.txt"
    f.write_text("hello world\n")

    assert grep_content(f, "nope") == "no matches found"


def test_grep_content_ignore_case(tmp_path):
    f = tmp_path / "a.txt"
    f.write_text("FOO bar\n")

    result = grep_content(f, "foo", ignore_case=True)
    assert "FOO bar" in result


def test_grep_content_directory_requires_recursive(tmp_path):
    (tmp_path / "a.txt").write_text("foo\n")

    with pytest.raises(IsADirectoryError, match="set recursive=true"):
        grep_content(tmp_path, "foo")


def test_grep_content_recursive_searches_subdirectories(tmp_path):
    (tmp_path / "a.txt").write_text("hello\n")
    sub = tmp_path / "sub"
    sub.mkdir()
    (sub / "b.txt").write_text("another foo line\n")

    result = grep_content(tmp_path, "foo", recursive=True)
    assert "b.txt:1:another foo line" in result


def test_grep_content_invalid_pattern(tmp_path):
    f = tmp_path / "a.txt"
    f.write_text("hello\n")

    with pytest.raises(ValueError, match="invalid pattern"):
        grep_content(f, "(")


def test_grep_content_blocks_path_traversal(tmp_path):
    outside = tmp_path.parent / "outside.txt"
    outside.write_text("secret")

    try:
        with pytest.raises(PermissionError, match="outside the allowed director"):
            grep_content(outside, "secret", allowed_root=tmp_path)
    finally:
        outside.unlink(missing_ok=True)


def test_grep_content_recursive_skips_symlinks_that_escape_root(tmp_path):
    outside_dir = tmp_path.parent / "grep_symlink_outside"
    outside_dir.mkdir(exist_ok=True)
    secret = outside_dir / "secret.txt"
    secret.write_text("top secret foo\n")

    sub = tmp_path / "sub"
    sub.mkdir()
    link = sub / "escape.txt"

    try:
        link.symlink_to(secret)
    except (OSError, NotImplementedError):
        pytest.skip("symlinks not supported")

    try:
        result = grep_content(tmp_path, "foo", recursive=True, allowed_root=tmp_path)
        assert "top secret" not in result
    finally:
        link.unlink(missing_ok=True)
        secret.unlink(missing_ok=True)
        outside_dir.rmdir()


def _call_with_timeout(fn, *args, timeout=5, **kwargs):
    """Run fn in a worker thread and fail loudly if it doesn't return in time.

    The whole point of a regression test for a hang bug is that the test
    itself must not be hangable: calling grep_content directly on the test
    thread would, on a regression, block the entire pytest run with no
    diagnostic (no pytest-timeout plugin is configured for this package).
    """
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
        future = executor.submit(fn, *args, **kwargs)
        return future.result(timeout=timeout)


def test_grep_content_recursive_skips_fifo_and_finds_matches_around_it(tmp_path):
    (tmp_path / "aaa_before.txt").write_text("foo before\n")
    fifo_path = tmp_path / "mmm_pipe"
    try:
        os.mkfifo(fifo_path)
    except (AttributeError, OSError):
        pytest.skip("FIFOs not supported")
    (tmp_path / "zzz_after.txt").write_text("foo after\n")

    try:
        result = _call_with_timeout(grep_content, tmp_path, "foo", recursive=True, allowed_root=tmp_path)
    except concurrent.futures.TimeoutError:
        pytest.fail("grep_content hung on a FIFO instead of skipping it")
    assert "aaa_before.txt:1:foo before" in result
    assert "zzz_after.txt:1:foo after" in result


def test_grep_content_single_target_fifo_raises_instead_of_hanging(tmp_path):
    fifo_path = tmp_path / "pipe"
    try:
        os.mkfifo(fifo_path)
    except (AttributeError, OSError):
        pytest.skip("FIFOs not supported")

    try:
        with pytest.raises(OSError, match="not a regular file"):
            _call_with_timeout(grep_content, fifo_path, "foo", allowed_root=tmp_path)
    except concurrent.futures.TimeoutError:
        pytest.fail("grep_content hung opening a FIFO directly instead of erroring")


def test_grep_content_no_matches_found_is_annotated_when_a_file_could_not_be_read(tmp_path):
    if os.geteuid() == 0:
        pytest.skip("root bypasses file permissions; cannot exercise this case")

    # An unreadable *file* (listed fine, but fails to open) exercises the
    # skip-counting path via grep_file's own except OSError handler.
    unreadable = tmp_path / "hidden.txt"
    unreadable.write_text("foo hidden\n")
    unreadable.chmod(0o000)

    try:
        result = grep_content(tmp_path, "foo", recursive=True, allowed_root=tmp_path)
    finally:
        unreadable.chmod(0o644)

    assert "no matches found" in result
    assert "could not be read" in result


def test_grep_content_recursive_does_not_abort_or_discard_matches_on_an_unreadable_subdirectory(tmp_path):
    if os.geteuid() == 0:
        pytest.skip("root bypasses directory permissions; cannot exercise this case")

    ok_sub = tmp_path / "aaa_ok"
    ok_sub.mkdir()
    (ok_sub / "match.txt").write_text("foo readable\n")

    noperm_sub = tmp_path / "mmm_noperm"
    noperm_sub.mkdir()
    (noperm_sub / "hidden.txt").write_text("foo hidden\n")
    noperm_sub.chmod(0o000)

    try:
        result = grep_content(tmp_path, "foo", recursive=True, allowed_root=tmp_path)
    finally:
        noperm_sub.chmod(0o755)

    assert "match.txt:1:foo readable" in result


def test_grep_content_no_matches_found_is_annotated_when_a_subdirectory_could_not_be_read(tmp_path):
    if os.geteuid() == 0:
        pytest.skip("root bypasses directory permissions; cannot exercise this case")

    # The only match lives inside a subdirectory that can't be listed. Before
    # switching from rglob() (which silently omits directories it can't
    # list, at any depth, with no hook to observe the failure) to os.walk's
    # onerror callback, this returned a bare "no matches found" -- a
    # confidently wrong empty result -- with no indication anything was
    # skipped.
    noperm_sub = tmp_path / "noperm"
    noperm_sub.mkdir()
    (noperm_sub / "hidden.txt").write_text("foo hidden\n")
    noperm_sub.chmod(0o000)

    try:
        result = grep_content(tmp_path, "foo", recursive=True, allowed_root=tmp_path)
    finally:
        noperm_sub.chmod(0o755)

    assert "no matches found" in result
    assert "could not be read" in result


def test_grep_content_recursive_on_fully_unreadable_root_raises_instead_of_empty_result(tmp_path):
    if os.geteuid() == 0:
        pytest.skip("root bypasses directory permissions; cannot exercise this case")

    (tmp_path / "hidden.txt").write_text("foo hidden\n")
    tmp_path.chmod(0o000)

    try:
        with pytest.raises(OSError, match="could not be read"):
            grep_content(tmp_path, "foo", recursive=True, allowed_root=tmp_path)
    finally:
        tmp_path.chmod(0o755)


def test_grep_content_truncates_long_matched_lines(tmp_path):
    long_line = "foo " + "x" * 3000
    f = tmp_path / "long.txt"
    f.write_text(long_line + "\n")

    result = grep_content(f, "foo", allowed_root=tmp_path)
    assert result.endswith("...")
    assert long_line not in result
    matched_line = result.split(":", 2)[-1]
    assert len(matched_line) < 2100


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
        patch.dict(
            "os.environ",
            {**env_overrides, "KAGENT_SRT_SETTINGS_PATH": "/config/srt-settings.json"},
            clear=True,
        ),
        patch("asyncio.create_subprocess_exec", side_effect=mock_exec),
    ):
        await execute_command("echo hello", working_dir=tmp_path)

    env = captured["env"]
    assert "OPENAI_API_KEY" not in env
    assert "ANTHROPIC_API_KEY" not in env
    assert "GOOGLE_APPLICATION_CREDENTIALS" not in env
    assert "AWS_ACCESS_KEY_ID" not in env
    assert env["HOME"] == "/home/user"
