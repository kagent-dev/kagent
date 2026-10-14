# DeepSeek Harness BYO reference integration

This image runs DeepSeek Harness (DSH) `0.2.0-rc.2` behind kagent's existing
`spec.byo` contract. It adds no CRD fields or native runtime type. The bridge
reads `KAGENT_CONFIG_JSON` and `KAGENT_AGENT_CARD_JSON`, uses the shared A2A
executor, and serves private gRPC on port 80 and readiness on port 8081.

## Supported configuration

- OpenAI-compatible Chat Completions ModelConfig, including a custom base URL
  and headers. Provider credentials remain gateway-injected Secret references.
- The compiled system prompt and one root conversation per Actor.
- Whole Streamable HTTP MCP servers with compiled headers, HTTP timeouts and
  session termination policy, enforced by a loopback Go MCP proxy.
- Text replies and text tool results, including tool errors.
- ACP cancellation and session resumption after the DSH process exits.
- DSH one-shot permission requests mapped to kagent's approval messages.

ACP emits committed semantic updates, rather than raw provider token deltas.
Context occupancy is not per-call token usage, so the bridge does not report
model token totals from ACP usage updates. DSH's own telemetry exporter and
auxiliary web tools are disabled; the kagent wrapper retains its telemetry path.

Unsupported configuration is rejected at startup: Responses API, model
generation overrides, API-key passthrough, custom TLS, SSE/stdio MCP bindings,
tool subsets, SSE read-timeout overrides, forwarded caller headers, and kagent `requireApproval` bindings.
DSH's ACP server has no per-MCP-server approval policy, so accepting that field
would not enforce the requested gate. DSH sandbox permission requests are a
separate channel and can still be answered through kagent.

The proxy discovers the remote tool catalog at each turn. It preserves the
CRD's default 30-second timeout and `terminateOnClose: true`; an explicit false
closes the local client without terminating the remote session. It does not
forward mid-turn catalog notifications, MCP resources, prompts or elicitation.

kagent skills, Agent Plugins, subagent bindings, memory, structured output and
ADK-specific context settings are not translated. DSH keeps its own built-in
tools and plugin composition; those are not equivalent to kagent Agent Plugins.
The first version accepts text input and output only.

Non-object tool input is retained under `args.rawInput` in tool-call events.
DSH validates the original input and returns tool errors to the model, allowing
recovery from arrays, scalars, null and malformed argument strings.

## State and process lifetime

Each turn starts a DSH ACP process and creates or resumes the Actor's private
DSH session. A pending permission request retains that process until the
decision arrives. Completion closes the session and stdin so DSH can flush its
state. Two nested Linux child subreapers own each process tree, including
orphaned tools and tools that create a separate session or process group.

Cancellation interrupts blocked ACP writes by closing stdin. After five seconds
of graceful shutdown, the adapter closes a private lifetime pipe to request forced
cleanup. Adapter death also closes that pipe. If the inner supervisor dies, the
outer guard adopts and reaps its descendants. If the guard dies, its lifetime
pipe closes and the inner supervisor kills DSH and reaps its descendants.
The adapter waits for an explicit cleanup confirmation before releasing the turn;
process exit or closed output pipes alone are insufficient.

During payload execution, this tolerates the loss of either supervisor. If neither
can confirm cleanup, for example after losing both, the adapter logs the failure
and leaves the task unsettled. Replacing the Actor restores process containment;
it does not reconcile the controller's outstanding task. Recovery also needs
controller-owned fencing and task reconciliation, tracked in
[kagent #3010](https://github.com/kagent-dev/kagent/issues/3010).
As a temporary DSH-only mitigation, cancellation with no live execution owner
returns an explicit `TASK_NOT_CANCELABLE` error instead of waiting indefinitely.
It leaves the stored task unchanged; it does not certify cleanup or unblock the
session's lifecycle operations.
Uninterruptible kernel I/O can also delay cleanup. This is
not a guarantee against simultaneous process kills or malicious same-UID tools.

Process supervision requires Linux with readable procfs child lists and permission
to set `PR_SET_CHILD_SUBREAPER`. It runs as the image's unprivileged user without
additional Linux capabilities. Unsupported hosts fail validation; run runtime
tests in Linux rather than launching this adapter natively on macOS. This manages
process lifetime; sandbox and container isolation remain platform responsibilities.

MCP HTTP redirects must retain the configured scheme, hostname and effective
port. Same-origin path redirects are supported, with a ten-redirect limit per
request. Cross-origin redirects, including HTTPS-to-HTTP redirects, are rejected
before sending a request or injecting binding headers.

`/data/adapter/state.json` binds the Actor to one native session. DSH session
logs and storage live under `/data/dsh`, and the workspace is `/data/workspace`.
The CLI profile and compiled overlay use a disposable home outside `/data`.
Do not delete the native session logs while retaining the continuation binding.
Public tasks and history remain in kagent's controller-backed TaskStore.

Pending approvals also need the live ACP process. They cannot currently recover
from a DATA-only snapshot or process replacement. The shared durable-input work
in [kagent #3165](https://github.com/kagent-dev/kagent/pull/3165) is a migration
dependency, but DSH must also support resuming the exact pending call without
replaying a tool before this adapter can adopt that recovery path.
Until then, the DSH adapter accepts a send referencing an existing task only
when that task owns the live pending approval. It rejects both approval decisions
and plain replies to a lost task before the SDK appends history or changes task
state. Continue completed work with a new A2A task in the same conversation.
These guards do not replay prompts or apply approvals after ownership is lost;
they provide explicit failure, not automatic approval recovery.

The local tests verify recovery after a DSH process restart. Cluster acceptance
also covers conversation and workspace continuity across automatic suspension
and explicit suspend/resume; Actor replacement and fork remain unverified.

## Build and deploy

From the repository root:

```sh
docker build -f go/harness/byo/dsh/Dockerfile -t YOUR_REGISTRY/kagent-dsh-byo:dev go
docker push YOUR_REGISTRY/kagent-dsh-byo:dev
```

Record the pushed image's manifest digest. The example requires the full
`YOUR_REGISTRY/kagent-dsh-byo@sha256:...` reference, not its tag.

Use a clean kagent installation whose Agent CRD supports `spec.template.inline`.
This example targets the API at source commit `b42a6c53`; `1.0.0-alpha9` uses
the earlier `templateRef` shape and rejects this Agent manifest. Cluster acceptance
used a control plane built from this checkout and Substrate `0.5.0-alpha2`.
Configure a `kagent-default`
WorkerPool, snapshot storage, and Substrate credential gateway. In namespace
`kagent`, create the Secret `deepseek-api-key` with an `OPENAI_API_KEY` entry
using your normal secret-management workflow. Grant the Substrate atespace
access to that namespace as described in
[credential injection](../../../../docs/architecture/credential-injection.md).
The bridge honors the platform's Node CA and proxy environment.

Set `DSH_MODEL` to a Chat Completions model available to your DeepSeek account,
`DSH_BYO_IMAGE` to the image digest, and `SNAPSHOT_LOCATION` to your installation's
snapshot storage URI. Render only those variables:

```sh
envsubst '${DSH_MODEL} ${DSH_BYO_IMAGE} ${SNAPSHOT_LOCATION}' \
  < examples/dsh-byo/agent.yaml.tmpl > /tmp/dsh-agent.yaml
kubectl apply --dry-run=server -f /tmp/dsh-agent.yaml
kubectl apply -f /tmp/dsh-agent.yaml
kubectl get harness,agent -n kagent
```

After preparation succeeds, open `dsh-agent` in the kagent UI and create a
conversation. To bind an MCP server, add a normal `tools[].mcp` binding to the
inline AgentTemplate, omit the `tools` subset, and leave `requireApproval` unset.
The server must use Streamable HTTP and the platform trust bundle.

## Verification

From `go/` in a Linux development environment:

```sh
go test -race ./harness/byo/dsh/... ./harness/runtime/...
```

The Linux subprocess tests exercise permission decisions, cancellation, identity
validation, malformed protocol handling, detached and orphaned tool cleanup,
unrelated-process survival, and adapter death without a model API. Configuration,
event conversion and wire tests also run on non-Linux hosts.
For the published runtime tests, install the locked npm dependencies and run:

```sh
npm ci --prefix harness/byo/dsh --ignore-scripts --no-audit --no-fund
DSH_TEST_BIN="$PWD/harness/byo/dsh/node_modules/.bin/dsh" \
  go test -race ./harness/byo/dsh/internal/bridge -run TestRealDSH -count=1
```

These tests use a real DSH process, a local model fixture, and a local MCP
server. A2A gRPC calls use the production TaskStore client and settlement wrapper
against a local controller RPC fixture. They check MCP execution, resumed turns,
runtime identity headers and versioned settlement without provider credentials.
Separate process tests require DSH and detached tools to exit before settlement
on normal completion, forced cleanup and cancellation.

The durable-workspace test creates, reads and edits a file with DSH's built-in
tools across three fresh adapter instances. It verifies that the native session
identity, conversation history and workspace contents survive reconstruction
from the same durable directory. These checks do not exercise the real controller
database or Substrate snapshots and are not a substitute for cluster acceptance.

Manual cluster acceptance on Linux arm64 with gVisor, Kubernetes `1.37.0` and
Substrate `0.5.0-alpha2` exercised the public A2A API and real PostgreSQL-backed
TaskStore. Three turns completed and automatically suspended; public `GetTask`
returned the completed task after each suspension. After explicit suspend/resume,
the model fixture received the earlier conversation history. Gateway credential
injection and workspace write/read/edit/read across suspensions also passed.
The model was a local Chat Completions fixture, not a live provider.

Before release, complete the remaining cluster checks: MCP success and failure,
cancellation followed by another turn, approval allow/reject/cancel, and continuity
after Actor replacement and fork. The checks above do not establish guarantees
for untested lifecycle paths or model usage reporting.
