# Working in sandboxes

Use the kagent CLI when available for command execution and local file transfer,
or the connected MCP tools for direct tool calls. The CLI calls gRPC directly;
MCP is a separate adapter to the same sandbox service.
MCP names below are server tool names; a client may add a server prefix. Check
installed command help and advertised schemas for the connected deployment.

## CLI workflow

`kagent sandbox --help` lists installed support. Commands reuse the CLI's
`--api-url`, `--namespace` (`-n`), `--user-id`, `--ca-file`, and `--server-name`
settings. Use the configured endpoint and identity; do not substitute another
user's identity to work around an authorization failure.

1. Discover with `kagent sandbox templates -n NAMESPACE -o json`. The result
   includes template references, workload images, and full resources for readiness
   inspection. `kagent sandbox list -o json` lists your existing sandboxes.
2. Generate and retain a unique request ID. Create with
   `kagent sandbox create TEMPLATE -n NAMESPACE --request-id REQUEST_ID --ttl 15m -o json`.
   The command makes one attempt. On a transient failure, retry with identical
   inputs and the same request ID; inspect `state`, `operation`, and `expiresAt`.
3. Upload with `kagent sandbox upload SANDBOX_ID ./input.json input.json`.
   Uploads replace remote files and support regular files up to 64 MiB. This path
   streams bytes directly without base64 in the model's context.
4. Execute with `kagent sandbox exec SANDBOX_ID -- python3 script.py` after
   uploading the script and confirming the template supplies `python3`.
   `--cwd` defaults to `/data/workspace`; `--env KEY=VALUE` sets process variables.
   The `--` separator is required before the executable and its arguments.
5. Download with `kagent sandbox download SANDBOX_ID summary.json ./summary.json`.
   A successful download atomically replaces the local destination. Failed
   downloads leave the previous destination intact. Remote writes can be partial.
6. Delete with `kagent sandbox delete SANDBOX_ID` after collecting artifacts.
   `get`, `suspend`, and `resume` take the same sandbox ID.

`exec` starts once, waits by default, streams stdout/stderr, and returns the
remote exit code (or a nonzero CLI error if observation fails). `--timeout`
bounds the command, including waiting. A timeout or interrupt stops observing;
it does not kill the remote process. Retain the printed process ID and use
`kagent sandbox wait SANDBOX_ID PROCESS_ID --stdout-offset N --stderr-offset N`
to continue from the reported offsets. `exec --wait=false` returns after start;
`process SANDBOX_ID PROCESS_ID` inspects and `kill SANDBOX_ID PROCESS_ID` stops it.

For automation, `exec` and `wait` with `-o json` emit newline-delimited records:
`started`, `output`, `finished`, or `interrupted`. Each contains `sandbox_id`,
`process_id`, and stdout/stderr byte offsets. Output records contain `source` and
base64 `data`; finished records contain `status` and `exit_code`. Other commands
emit one JSON value; protobuf fields use camelCase, unlike MCP's snake_case.
The process ID in the start record remains usable if waiting fails.

## Choose the environment

Call `list_sandbox_templates`, optionally with `namespace`. It returns
`templates`, each containing `namespace` and `name`; it does not report installed
programs or readiness. When Kubernetes access is available, inspect the chosen
template with `kubectl get sandboxtemplate NAME -n NAMESPACE -o yaml`. Check its
workload image, `status.conditions` (`Ready`), and observed generation. Otherwise
use known template capabilities and verify required programs after creation.

Choose tasks that can run with the tools image's existing software and supplied
files: standard-library scripts, small reproductions, offline tests, data
transformations, and artifact generation. Sandboxes currently have no allowed
egress destinations; `git clone`, dependency installation, and external API calls
cannot be assumed to work. Your local checkout and credentials are not mounted
automatically. Upload the task's inputs explicitly; keep working files under
`/data/workspace`, the default process working directory and file API root.

## MCP tools

Clients that support MCP prompts can retrieve `sandbox-task` with required
`task` and optional `namespace`/`template` (template requires namespace), or
`sandbox-recovery` with `sandbox_id`. Retrieval returns workflow messages without
running tools. Server instructions describe the overall workflow; tool
descriptions carry essential guidance for clients that only expose tools.

Fields in the required column must be supplied. File contents and output bytes
use standard base64, including for text. Sandbox IDs are UUIDs returned by kagent.

| Tool | Required inputs | Optional inputs / result |
| --- | --- | --- |
| `list_sandbox_templates` | none | `namespace`; returns `templates` references |
| `create_sandbox` | `namespace`, `template`, `request_id` | `name`, `ttl_seconds`; returns a sandbox summary |
| `list_sandboxes` | none | `page_size`, `page_token`; returns `sandboxes`, `next_page_token` |
| `get_sandbox`, `suspend_sandbox`, `resume_sandbox`, `delete_sandbox` | `sandbox_id` | Return a sandbox summary |
| `start_sandbox_process` | `sandbox_id`, `command` (argv array) | `cwd`, `env` (string map); returns `process_id` |
| `get_sandbox_process`, `kill_sandbox_process` | `sandbox_id`, `process_id` | Get returns `status` and `exit_code`; Kill returns `exit_code` |
| `read_sandbox_outputs` | `sandbox_id`, `process_id` | `stdout_offset`, `stderr_offset`; returns base64 streams, continuation offsets, `truncated` |
| `write_sandbox_file` | `sandbox_id`, `path`, `data_base64` | `mode` (decimal Unix bits, e.g. `420` for `0644`); returns `bytes_written` |
| `read_sandbox_file` | `sandbox_id`, `path` | Returns `data_base64` |

Sandbox summaries contain `id`, `sandbox_template`, `state`, `operation`,
`expires_at`, and optional `name` and `failure`. MCP returns the summary directly,
without the gRPC response's `sandbox` wrapper. Check MCP `isError` before using a
result; a completed tool call can carry a service error.

## Task workflow

1. **Create or reuse.** Select the namespace and template from discovery. Generate
   a unique `request_id` once per new sandbox and retain the complete creation
   input for retries. Choose a TTL covering execution and artifact retrieval;
   omission uses operator policy (normally one hour, maximum normally 24 hours).
   Check `expires_at` and require `RUNTIME_STATE_READY` with
   `RUNTIME_OPERATION_NONE` before guest operations. Resume a task's suspended
   sandbox when needed.
2. **Stage inputs.** Write source, data, or an archive with `write_sandbox_file`.
   Decode/encode bytes using available tools, and verify `bytes_written`. MCP
   reads and writes accept at most 1 MiB of decoded file data per call. There is
   no file-offset or append input: repeated writes replace the destination.
3. **Execute.** Pass executable and arguments as `command`, with an explicit
   `cwd` when useful. Shell syntax needs an explicit shell, e.g.
   `["sh", "-c", "command > result.txt"]`. The executable must exist in the
   template's image. Each start returns immediately with a new process handle.
4. **Observe.** Retain `process_id`; poll `get_sandbox_process` with a bounded
   delay and task deadline. `PROCESS_STATUS_RUNNING` is not completion and its
   `exit_code` is not yet meaningful. Terminal statuses are
   `PROCESS_STATUS_COMPLETED`, `PROCESS_STATUS_FAILED`, and
   `PROCESS_STATUS_TERMINATED`; inspect the exit code and outputs before claiming
   success. Use `kill_sandbox_process` when the task needs to stop the command.
5. **Collect.** Decode `stdout_base64` and `stderr_base64` from
   `read_sandbox_outputs`. It reads currently available output, up to 1 MiB
   combined, without following future output. Pass both returned byte offsets
   into subsequent reads; continue when truncated and read again after process
   completion. Empty output or `truncated: false` does not mean the process ended.
   Read result files, decode them, and save deliverables into the user's working
   environment before cleanup. A path inside a sandbox is not a local artifact.
6. **Finish.** Delete scratch sandboxes created for the task after retrieving
   results. Reused or deliberately retained sandboxes need an explicit handoff
   of their ID, expiration, and artifact paths. If cleanup fails, report it and
   retain the ID for retry; do not claim deletion succeeded.

For files larger than the MCP limit, use an available gRPC streaming client
(64 MiB transfer limit), or transfer separate smaller files and combine/split them
with installed guest tools. Do not invent pagination fields for file tools.
Store substantial results as files: guest output capture is bounded too.

## Example: process supplied JSON

After discovering a template with `python3`, replace `NAMESPACE` and `TEMPLATE`
with that reference and `UNIQUE_REQUEST_ID` with a newly generated, retained ID.
Call `create_sandbox`:

```json
{"namespace":"NAMESPACE","template":"TEMPLATE","request_id":"UNIQUE_REQUEST_ID","name":"json-summary","ttl_seconds":900}
```

Check readiness as above. In the calls below, replace `SANDBOX_ID` with the
returned `id`. Call `write_sandbox_file` to upload the bytes `[2,3,5]\n`:

```json
{"sandbox_id":"SANDBOX_ID","path":"input.json","data_base64":"WzIsMyw1XQo="}
```

Call `start_sandbox_process`:

```json
{"sandbox_id":"SANDBOX_ID","command":["python3","-c","import json; from pathlib import Path; values = json.loads(Path('input.json').read_text()); Path('summary.json').write_text(json.dumps({'count': len(values), 'sum': sum(values)}))"],"cwd":"/data/workspace"}
```

Replace `PROCESS_ID` with the returned `process_id` and call
`get_sandbox_process` until terminal:

```json
{"sandbox_id":"SANDBOX_ID","process_id":"PROCESS_ID"}
```

Inspect output with `read_sandbox_outputs` (repeat using returned offsets as
needed):

```json
{"sandbox_id":"SANDBOX_ID","process_id":"PROCESS_ID","stdout_offset":0,"stderr_offset":0}
```

After successful completion, call `read_sandbox_file` and decode `data_base64`:

```json
{"sandbox_id":"SANDBOX_ID","path":"summary.json"}
```

The result should be `{"count": 3, "sum": 10}`. Save it locally or include it in
the task's answer, then call `delete_sandbox`:

```json
{"sandbox_id":"SANDBOX_ID"}
```

## Recovery and lifetime

- **Lifecycle retries:** Create, Suspend, Resume, and Delete execute one attempt
  inline. On a transient failure, repeat the same mutation with capped backoff
  and an overall task deadline. Create retries must preserve `request_id` and
  every input; a new ID can allocate another sandbox. Get/List only observe and
  never advance pending work. Serialize lifecycle mutations for a sandbox.
- **Correctable errors:** Fix invalid input, missing credentials, access denial,
  or template readiness before retrying. An active conflicting attempt can
  temporarily block progress. Inspect `state`, `operation`, and `failure`; if
  retrying cannot finish within the task deadline, report the pending operation
  and retain its identity. See `docs/lifecycle-retries.md` for gRPC error codes.
- **Uncertain process start:** StartProcess has no idempotency key. A timeout or
  lost response may mean the command already started. Inspect a known process
  ID or task result files before deciding whether another execution is safe;
  do not apply lifecycle retry logic to process starts.
- **Suspend/resume:** Suspension can interrupt commands and file transfers.
  Durable files survive resume, but process handles are not durable. Verify
  files after interruption; writes can leave partial data. Collect results
  before suspension when possible and start fresh processes after resume.
- **Expiration:** Activity and resume do not extend `expires_at`. Expiration
  rejects guest operations and triggers deletion, even for suspended sandboxes.
  Delete also removes files; a retained ID is not a backup. Replaying a creation
  request does not reset state, renew the TTL, or recreate a deleted sandbox.

## Enable sandbox tools for a kagent agent

Read this section for agent setup tasks. Helm registers a `RemoteMCPServer`
named `<fullname>-api` (`kagent-api` for the standard release) in the controller
namespace, pointing to the controller's `/mcp` endpoint. AgentTemplate MCP
references are same-namespace; another namespace needs its own registration.
Bind the required sandbox tools in the AgentTemplate's `spec.tools` (or an
Agent's inline `spec.template.tools`):

```yaml
tools:
  - mcp:
      server:
        kind: RemoteMCPServer
        name: kagent-api
      tools:
        - list_sandbox_templates
        - create_sandbox
        - get_sandbox
        - list_sandboxes
        - start_sandbox_process
        - get_sandbox_process
        - kill_sandbox_process
        - read_sandbox_outputs
        - write_sandbox_file
        - read_sandbox_file
        - delete_sandbox
```

Add `suspend_sandbox` and `resume_sandbox` when the agent needs that workflow.
Sandbox ownership follows the identity authenticated by the MCP connection.
For the Go kagent runtime, set `KAGENT_PROPAGATE_TOKEN=true` in the Harness's
`spec.env` when its trusted MCP servers should receive the invoking caller's
credentials. This opts configured servers into credential propagation; it is
not a sandbox-specific permission. Do not assume this Go runtime setting applies
to Codex or Claude. Session share tokens grant no sandbox access.

Creating or changing a SandboxTemplate is a separate setup operation. It requires
a SHA-256-pinned tools image and same-namespace Substrate configuration; kagent
supplies the guest entrypoint. It does not inherit a Harness. Use
`docs/architecture/sandboxes.md` and `go/api/v1alpha3/sandboxtemplate_types.go`
for its schema and preparation requirements.

For a gRPC client, use `kagent.api.v1alpha1.SandboxService` for lifecycle and
upstream `ateenv.v1alpha.ProcessService` / `FileSystemService` on the same
apiserver connection for guest work. Send exactly one `kagent-sandbox-id`
metadata value with the Sandbox UUID plus the connection's normal authentication.
Route through kagent; Actor IDs and private guest endpoints are not client APIs.

Source contracts: `go/core/internal/mcp/sandboxes.go`,
`proto/kagent/api/v1alpha1/sandboxes.proto`, and
`proto/ateenv/v1alpha/guest.proto`. Runtime limits and process semantics are in
`go/sandbox/guest/README.md`.
