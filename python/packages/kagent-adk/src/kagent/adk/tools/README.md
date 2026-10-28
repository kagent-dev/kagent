# Built-in file and shell tools

ADK agents get `read_file`, `write_file`, `edit_file`, `list_files`, `grep_file`,
and `bash` independently of skills. Set `runtime.builtin_tools.disable_file_search`
or `disable_bash` to omit the corresponding tools.

The default working directory is `/data/workspace`. It belongs to the Actor,
is shared by its root agent and subagents, and survives VOLUMES snapshots and resume.
Go and Python use the same workspace layout; subdirectories are created as needed.
Paths follow normal filesystem semantics; Substrate is the security boundary.
Skills are discovered separately and use their configured absolute paths.

Each bash call starts a fresh shell, inherits the Actor environment, and returns
combined stdout and stderr. It defaults to 60 seconds; `timeout_seconds` can select
up to 600 seconds. Output capture is capped at 64 KiB and marked when truncated.
Redirect large output to a file to inspect the complete result. On failure, the
exit code and captured output are retained. Child processes are stopped on return,
timeout, or cancellation. Use workspace files for durable work, not background jobs.

`bash_venv_path` prepends that environment's `bin` directory to `PATH` and sets
`VIRTUAL_ENV`. The workspace and optional skills directory are added to `PYTHONPATH`.
Shell helpers do not log command payloads or output.

Python's regular expression engine can backtrack indefinitely. Its `grep_file`
keeps a worker-process timeout for this reason; Go uses its linear-time regexp
engine. Recursive searches do not follow symlinked subdirectories.

File reads default to 2000 lines. Read, list, and grep results are capped at
64 KiB; use read offsets or narrower searches for additional content. Edits
require a nonempty `old_string`, which must be unique unless `replace_all=true`.

## Skills and artifacts

Go and Python ADK use their upstream skills toolsets. Python exposes
`list_skills`, `load_skill`, and `load_skill_resource`; scripts run through the
common bash tool. A small prompt adapter directs scripts to bash when available
and describes the execution limit when it is absent. Skill discovery, loading,
and resource access remain upstream implementations. Skills are registered only
from each agent's configured `skills_directory`. Subagents do not implicitly load `/skills`.

The unused custom Python skills bundle and artifact staging/return tools have
been removed. Python retains ADK's upstream artifact service for native A2A
integration. The workspace does not precreate `uploads/` or `outputs/`.
