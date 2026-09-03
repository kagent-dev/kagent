# Claude Harness

The Claude Harness runs Claude Code as a native Kagent runtime. It compiles an
`AgentTemplate` into Claude Code configuration, runs each turn in a Substrate
Actor, and exposes the result through Kagent's A2A API.

## Code structure

The controller and Actor share only the versioned config contract. Kubernetes
resolution stays in the controller; native process behavior stays in this
harness.

| Path | Look here for |
| --- | --- |
| [`../../core/internal/translator/claude`](../../core/internal/translator/claude) | Translating `Harness`, `AgentTemplate`, model, MCP, plugin, and Secret inputs into a runtime revision and warnings |
| [`config/config.go`](config/config.go) | The versioned JSON contract shared by the compiler and runtime, including defaults and reserved environment variables |
| [`cmd/main.go`](cmd/main.go) | Actor startup, environment inputs, Claude version validation, continuation-store wiring, and private A2A startup |
| [`internal/adapter/adapter.go`](internal/adapter/adapter.go) | Materializing Claude home, skills, MCP config, and ephemeral provider credentials |
| [`internal/driver`](internal/driver) | Claude CLI arguments, stream-JSON parsing, runtime-event translation, cancellation, and process supervision |

## Working

- [x] Anthropic, Amazon Bedrock, and Vertex AI model providers
- [x] Streaming text, tool calls, and tool results over A2A
- [x] Task cancellation
- [x] Durable Claude session resume between turns
- [x] Claude Code built-in tools
- [x] Shared local subagents
- [x] Standalone skills and plugin-provided skills
- [x] Direct HTTP and SSE MCP servers with whole-server tool access

## Planned / not yet supported

- [ ] Human-in-the-loop MCP approval with a live parked permission request and
      full-memory Actor pause/resume
- [ ] Checkpoint and fork continuity for Claude sessions
- [ ] Enforced selection of individual tools from an MCP server
- [ ] Dedicated subagents running in separate AgentInstances
- [ ] Skills, MCP tools, and nested subagents on local subagents
- [ ] Configuring Claude Code permission mode and trust boundary in Harness CRD

## Example usage

```yaml
apiVersion: kagent.dev/v1alpha3
kind: Harness
metadata:
  name: claude-e2e
  namespace: kagent
spec:
  claude: {}
  workload:
    image: ${KAGENT_CLAUDE_IMAGE}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
  allowedAgentTemplates:
    selector:
      matchLabels:
        kagent.dev/e2e-runtime: claude
---
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  labels:
    kagent.dev/e2e-runtime: claude
  name: kagent-claude
  namespace: kagent
spec:
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
```
