# Sandbox details

This file gives the sandbox behavior that `kagent sandbox <command> --help` does not show,
and the sandbox tools that an agent can bind.

## Choose a template

`kagent sandbox templates -o json` gives the namespace, name, and image of each template.
It does not list the programs in the image. After create, check a program:

```sh
kagent sandbox exec ID -- sh -c 'command -v python3'
```

- The sandbox has no network access unless its template allows it. Do not expect `git clone`,
  package installs, or external API calls to work.
- The sandbox does not get your local checkout or credentials. Upload each input file.

## Create and retry

`create`, `suspend`, `resume`, and `delete` each make one attempt.

1. Make a new request ID for each new sandbox. Record it with the template, `--ttl`, and `--name`.
2. Read `state` and `operation` in the JSON output.
3. If `state` is not `RUNTIME_STATE_READY` or `operation` is not `RUNTIME_OPERATION_NONE`,
   run the same `create` again with the same request ID and inputs.
4. On a transient error, retry the same command with a backoff and a deadline.

- A new request ID can make a second sandbox.
- `get` and `list` only read state. They do not move pending work forward.
- Run one lifecycle command at a time for each sandbox.

## Run a command

```sh
kagent sandbox exec ID --cwd /data/workspace --env MODE=test -- sh -c 'make test > test.log 2>&1'
```

- `exec` starts the process one time. The CLI does not retry an uncertain start.
- If `exec` fails before the process ID shows, the command can still run.
  Check the result files before you run it again.
- Shell syntax needs an explicit shell, as in `sh -c '...'`.
- In table mode, the CLI writes the process ID and the exit code to stderr.

The global `--timeout` (default `5m`) limits the full `exec` command. A timeout or Ctrl-C
stops the wait only. The remote process continues.

1. Record the process ID and the stdout and stderr offsets from the error.
2. Continue with `kagent sandbox wait ID PROCESS_ID --stdout-offset N --stderr-offset N`.
3. For a long job, add `--timeout 30m`, or use `exec --wait=false` and then `wait`.

- `kagent sandbox process ID PROCESS_ID` shows the state without a wait.
- `kagent sandbox kill ID PROCESS_ID` stops the process and its children.

With `-o json`, `exec` and `wait` write one JSON record on each line:

| `event` | Fields |
| --- | --- |
| `started` | `sandbox_id`, `process_id`, `stdout_offset`, `stderr_offset` |
| `output` | The fields above, plus `source` (`stdout` or `stderr`) and base64 `data` |
| `finished` | The fields above, plus `state` and `exit_code` |
| `interrupted` | The fields above. Use the offsets with `wait`. |

## Move files

- `upload` replaces the remote file. A failed upload can leave a partial file.
- `download` replaces the local file only after a full transfer.
- The limit is 64 MiB for each file. Split larger data with tools in the image.
- Keep large results in files. Do not print them to stdout.

## Lifetime

- If you do not set `--ttl`, the operator policy applies.
  The usual default is 1 hour. The maximum is 24 hours.
- Activity and `resume` do not extend the expiry time. Read `expiresAt` in the JSON output.
- At expiry, kagent deletes the sandbox and its files, also when it is suspended.
- `suspend` can stop running commands and file transfers. Files stay. Process IDs do not.
- After `resume`, check your files and start new processes.

## Finish

1. Download each result that the user needs. A path in the sandbox is not a local file.
2. Delete each sandbox that you made for the task.
3. If the user wants to keep a sandbox, report its ID, expiry time, and file paths.
4. If `delete` fails, report the error and the sandbox ID. Do not say that the delete succeeded.

## Sandbox tools

Bind these from server `kagent-api` in the [Agent manifest](agent-reference.md#agent-manifest) when the agent needs
an isolated workspace. They change only the sandbox.

- A usual set is `list_sandbox_templates`, `create_sandbox`, `get_sandbox`, `write_sandbox_file`, `start_sandbox_process`,
  `get_sandbox_process`, `read_sandbox_outputs`, `read_sandbox_file`, and `delete_sandbox`.
- Put them in one binding. Ask once whether `start_sandbox_process` needs approval.
- Do not bind the `shell` tool of `kagent-tool-server` for a workspace. It runs in the shared tool-server pod.
- `kagent sandbox templates` must show a template in `-n`. If it is empty, an admin creates a SandboxTemplate first.
- The agent's sandbox calls use the identity of its connection to `kagent-api`. To act as the invoking user, an admin
  sets the Helm value `config.runtimeDefaults.tokenPropagation.enabled=true`. It applies to the `kagent` runtime only.
