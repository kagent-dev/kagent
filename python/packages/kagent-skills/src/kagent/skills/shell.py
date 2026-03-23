"""Core, framework-agnostic logic for system tools (file and shell operations)."""

from __future__ import annotations

import asyncio
import logging
import os
import re
from pathlib import Path

logger = logging.getLogger(__name__)


# --- File Operation Tools ---


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
        if len(line) > 2000:
            line = line[:2000] + "..."
        result_lines.append(f"{i:6d}|{line}")

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


# --- UNCOMMENT 3: Add _get_srt_settings_args function ---
# def _get_srt_settings_args() -> list[str]:
#     """Return srt CLI args for --settings if a settings file is configured, else []."""
#     path = os.environ.get("KAGENT_SRT_SETTINGS_PATH", "")
#     if path:
#         return ["--settings", path]
#     return []


def _get_command_timeout_seconds(command: str) -> float:
    """Determine appropriate timeout for a command."""
    if "python " in command or "python3 " in command:
        return 60.0  # 1 minute for python scripts
    else:
        return 30.0  # 30 seconds for other commands


async def execute_command(command: str, working_dir: Path, skills_dir: Path = Path("/skills")) -> str:
    """Executes a shell command in a sandboxed environment."""
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
        # --- UNCOMMENT 3: Pass srt settings args ---
        # srt_args = _get_srt_settings_args()
        process = await asyncio.create_subprocess_exec(
            "srt",
            # *srt_args,
            "sh",
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
