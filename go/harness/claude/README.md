# Claude Harness

The Claude Harness runs Claude Code as a native Kagent runtime. It compiles an
`AgentTemplate` into Claude Code configuration, runs each turn in a Substrate
Actor, and exposes the result through Kagent's A2A API.

## Code structure

The controller and Actor share only the versioned config contract. Kubernetes
resolution stays in the controller; native process behavior stays in this
harness.

| Path                                                                             | Look here for                                                                                                         |
| -------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| [`../../core/internal/translator/claude`](../../core/internal/translator/claude) | Translating `Harness`, `AgentTemplate`, model, MCP, plugin, and Secret inputs into a runtime revision and warnings    |
| [`config/config.go`](config/config.go)                                           | The versioned JSON contract shared by the compiler and runtime, including defaults and reserved environment variables |
| [`cmd/main.go`](cmd/main.go)                                                     | Actor startup, environment inputs, Claude version validation, continuation-store wiring, and private A2A startup      |
| [`internal/adapter/adapter.go`](internal/adapter/adapter.go)                     | Materializing Claude home, skills, MCP config, and ephemeral provider credentials                                     |
| [`internal/driver`](internal/driver)                                             | Claude CLI arguments, stream-JSON parsing, runtime-event translation, cancellation, and process supervision           |

## Working

- [x] Anthropic API keys and Amazon Bedrock bearer tokens, injected by the gateway
- [x] Streaming text, tool calls, and tool results over A2A
- [x] Task cancellation
- [x] Durable Claude session resume between turns
- [x] Claude Code built-in tools
- [x] Shared local subagents
- [x] Standalone skills and plugin-provided skills
- [x] Direct HTTP and SSE MCP servers with whole-server tool access
- [x] Human-in-the-loop MCP tool approval across DATA-only restart
- [x] Native ask-user questions through durable deferred input

Credentials use [Substrate gateway injection](../../../docs/architecture/credential-injection.md).
AWS IAM keys and Vertex service-account keys require local signing and are rejected
by the compiler. Harness environment entries accept only literal values.

Production runs retain Claude's native retry defaults. E2E fixtures opt into
zero retries through an internal test switch while upstream denial handling
still causes minutes of waiting.

## Telemetry

The driver passes each prompt to Claude Code on stdin as stream-JSON. When
Claude Code telemetry is on, it holds the prompt until that telemetry has
initialized. Claude Code starts a turn without waiting for its telemetry, and
with first-party Anthropic credentials that initialization first waits on a
managed settings fetch. A turn that starts earlier drops its spans, events and
metrics, and ignores `TRACEPARENT`. The driver therefore adds a loopback
Prometheus metrics reader and sends the prompt once `claude_code.session.count`
appears there, which Claude Code increments only after its providers are
registered. That ordering is a Claude Code implementation detail, verified on
2.1.260, 2.1.282 and 2.1.285.

The wait is bounded at 10 seconds, so a stalled settings fetch delays a turn by
at most that much. The driver then sends the prompt and records a
`kagent.claude.telemetry_not_ready` event on the turn's `invoke_agent` span, as
it also does when it cannot reserve the loopback port. Claude Code stops waiting
for the fetch after 30 seconds, so in a turn that runs past then, spans started
after its telemetry initializes become roots of new traces rather than being
absent.

The gate delays new stdin prompts only. Deferred replay starts automatically on
`--resume`, so the gate cannot ensure telemetry is initialized before replay.

## Human-in-the-loop approval flow

Claude runs in print mode with compiler-owned `PreToolUse` hooks. Protected MCP
calls and `AskUserQuestion` return `permissionDecision: "defer"` before execution.
Claude exits with the pending call preserved in its transcript. The driver saves
A2A task ownership and the pending input, then returns `input-required` after
reaping the process.

After DATA restore, the shared executor recovers ownership and the harness starts
`claude -p --resume <session-id>` without adding a new user prompt. The hook reads
the saved approval or answer. Explicit ask rules also consult the private MCP
permission host; it returns only that matching saved decision. Ask-user answers
are supplied through `updatedInput`.

Cancellation resolves the native deferred call with a stop decision before
writing a tombstone.

The native [defer documentation](https://code.claude.com/docs/en/hooks#defer-a-tool-call-for-later)
describes the transcript-based continuation.

## Key limitations

- **Tool parenting is not retained across DATA suspension.** Resumed invocations
  link to the task's original invocation. Deferred tool spans use the resumed
  invocation and exclude approval wait time. Other tool spans still open when
  input is published are settled as unfinished; previous tool parents are not
  restored. Parenting within an execution segment is retained.
  Deferred span duration includes replay startup and is not exact tool-only time.

- **Parallel approvals are not supported.** Protected calls that Claude cannot
  defer in a parallel batch are denied through the permission fallback. A
  remaining single call can still be deferred and approved.

## Planned / not yet supported

- [ ] Enforced selection of individual tools from an MCP server
- [ ] Dedicated subagents running in separate Sessions
- [ ] Skills, MCP tools, and nested subagents on local subagents
- [ ] Configuring Claude Code permission mode and trust boundary in Harness CRD

## Example usage

The platform-managed `Harness` selects the Claude runtime, and the `Agent`
references it while defining behavior inline. The referenced ModelConfig and
RemoteMCPServer must already exist in `kagent`. The installed release supplies
the default Harness image. To override it, set `workload.image` to a full
`@sha256:`-pinned reference.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Harness
metadata:
  name: claude
  namespace: kagent
spec:
  claude: {}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: kagent-claude
  namespace: kagent
spec:
  template:
    inline:
      description: test
      modelConfig:
        name: bedrock-claude # Assuming you have created a modelconfig using Bedrock Anthropic
      systemPrompt: |
        Follow the selected skill and use the configured MCP tool.
      tools:
        - mcp:
            server:
              kind: RemoteMCPServer
              name: kagent-tool-server
      plugins:
        - source:
            git:
              url: https://github.com/agentplugins/agent-plugins-example.git
              commit: 5f3f5084a821aefa792e79500dd8f0462ab83473
          skills:
            - migrate-agent-plugin
  harnessRef:
    name: claude
```
