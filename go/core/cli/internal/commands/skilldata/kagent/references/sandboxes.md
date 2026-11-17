# Sandbox details

## Choose a template

The template list does not show the programs in the image. After create, check one:

```sh
kagent sandbox exec ID -- sh -c 'command -v python3'
```

- The sandbox does not get your local checkout or credentials. Upload each input file.

## Create and retry

- Retry the same `create`, with the same request ID and inputs and a backoff, until `state` is
  `RUNTIME_STATE_READY` and `operation` is `RUNTIME_OPERATION_NONE`.
- A new request ID makes a second sandbox.
- Run one lifecycle command at a time for each sandbox.

## Run a command

```sh
kagent sandbox exec ID --cwd /data/workspace --env MODE=test -- sh -c 'make test > test.log 2>&1'
```

- If `exec` fails or times out, follow the error. Check the result files before you run it again.
- For a long job, add `--timeout 30m`, or use `exec --wait=false` and then `kagent sandbox wait`.

## Move files

- `upload` replaces the remote file. A failed upload can leave a partial file.
- The limit is 64 MiB for each file. Split larger data with tools in the image.
- Keep large results in files. Do not print them to stdout.

## Lifetime

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
- By default, the agent's sandbox calls use the identity of its connection to `kagent-api`, not the invoking user.
