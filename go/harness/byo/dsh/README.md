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

## State and process lifetime

Each turn starts a DSH ACP process and creates or resumes the Actor's private
DSH session. A pending permission request retains that process until the
decision arrives. Completion closes the session and stdin so DSH can flush its
state; cancellation is followed by bounded process-group cleanup.

`/data/adapter/state.json` binds the Actor to one native session. DSH session
logs and storage live under `/data/dsh`, and the workspace is `/data/workspace`.
The CLI profile and compiled overlay use a disposable home outside `/data`.
Do not delete the native session logs while retaining the continuation binding.
Public tasks and history remain in kagent's controller-backed TaskStore.

The local tests verify recovery after a DSH process restart. Full Substrate
snapshot/restore remains a separate cluster acceptance check.

## Build and deploy

From the repository root:

```sh
docker build -f go/harness/byo/dsh/Dockerfile -t YOUR_REGISTRY/kagent-dsh-byo:dev go
docker push YOUR_REGISTRY/kagent-dsh-byo:dev
```

Record the pushed image's manifest digest. The example requires the full
`YOUR_REGISTRY/kagent-dsh-byo@sha256:...` reference, not its tag.

Use a clean kagent 1.x alpha installation with a configured `kagent-default`
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

From `go/`:

```sh
go test -race ./harness/byo/dsh/... ./harness/runtime/...
```

The subprocess tests exercise permission decisions, cancellation, identity
validation, malformed protocol handling and cleanup without a model API.
For the published runtime tests, install the locked npm dependencies and run:

```sh
npm ci --prefix harness/byo/dsh --ignore-scripts --no-audit --no-fund
DSH_TEST_BIN="$PWD/harness/byo/dsh/node_modules/.bin/dsh" \
  go test -race ./harness/byo/dsh/internal/bridge -run TestRealDSH -count=1
```

These tests use a real DSH process, a local model fixture, and a local MCP
server. They verify MCP execution and resumed turns through A2A gRPC without
provider credentials. The transport test substitutes an SDK-local TaskStore
for the controller, so it does not prove full cluster settlement or snapshots.

Before release, run the same scenarios in Substrate: two turns, MCP success and
failure, cancellation followed by another turn, approval allow/reject/cancel,
and conversation continuity after explicit suspend/resume and Actor replacement.
Do not advertise snapshot, fork or model-usage guarantees until those checks pass.
