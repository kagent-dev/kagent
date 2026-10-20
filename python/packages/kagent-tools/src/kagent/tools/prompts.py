def get_read_file_description() -> str:
    """Returns the standardized description for the read_file tool."""
    return """Reads a file from the filesystem with line numbers.

Usage:
- Provide a path to the file (absolute or relative to your working directory)
- Returns content with line numbers (format: LINE_NUMBER|CONTENT)
- Reads default to 2000 lines and results are capped at 64 KiB; use offset/limit to page
- Lines longer than 2000 characters are truncated
- Always read a file before editing it
- Paths are relative to /data/workspace unless absolute
"""


def get_write_file_description() -> str:
    """Returns the standardized description for the write_file tool."""
    return """Writes content to a file on the filesystem.

Usage:
- Provide a path (absolute or relative to working directory) and content to write
- Overwrites existing files
- Creates parent directories if needed
- For existing files, read them first using read_file
- Prefer editing existing files over writing new ones
- You can write to any writable location in the sandbox
- Filesystem access is governed by the sandbox
"""


def get_edit_file_description() -> str:
    """Returns the standardized description for the edit_file tool."""
    return """Performs exact string replacements in files.

Usage:
- You must read the file first using read_file
- Provide path (absolute or relative to working directory)
- When editing, preserve exact indentation from the file content
- Do NOT include line number prefixes in old_string or new_string
- old_string must be unique unless replace_all=true
- Use replace_all to rename variables/strings throughout the file
- old_string and new_string must be different
- Filesystem access is governed by the sandbox
"""


def get_list_files_description() -> str:
    """Returns the standardized description for the list_files tool."""
    return """Lists files and directories at a given path.

Usage:
- Provide a path (absolute or relative to your working directory); defaults to the working directory
- Directories are listed with a trailing "/"; files are followed by their size in bytes
- Paths are relative to /data/workspace unless absolute; results are capped at 64 KiB
"""


def get_grep_file_description() -> str:
    """Returns the standardized description for the grep_file tool."""
    return """Searches for a regular expression pattern in a file or directory.

Usage:
- Provide a pattern and a path (absolute or relative to your working directory)
- Set recursive=true to search all files under a directory path
- Recursion does not follow symlinked subdirectories; target a symlink directly to search it
- Set ignore_case=true for case-insensitive matching
- Returns matching lines as path:line_number:content
- Paths are relative to /data/workspace unless absolute; results are capped at 64 KiB
"""


def get_bash_description(file_search_tools: bool = False) -> str:
    """Returns the standardized description for the bash tool."""
    description = """Execute a bash command inside the agent sandbox.

- Working directory: /data/workspace. Files there survive suspend/resume and checkpoints.
- Each call starts a new shell; use cd inside a command or absolute paths.
- Default timeout: 60 seconds. Set timeout_seconds up to 600 for longer commands.
- Returns combined stdout and stderr, capped at 64 KiB with a truncation marker.
- Redirect large output to a file if you need to inspect it in full.
- Child processes are stopped when the call finishes or is canceled.
"""
    if file_search_tools:
        description += (
            "\nAlso available: list_files and grep_file, for exploring the filesystem without a full shell command.\n"
        )
    return description
