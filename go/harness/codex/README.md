# Codex Harness

The Codex Harness compiles a `kagent.dev/v1alpha3` `AgentTemplate` into a
compiler-owned Codex configuration and runs one Codex App Server `0.148.0`
process for each public A2A Task. Its native thread and workspace are retained
in the Actor's `DurableDir`.

Implemented support:

- OpenAI through a Secret-backed API key, the Responses API, and an optional
  absolute HTTP(S) base URL.
- Amazon Bedrock through either `AWS_BEARER_TOKEN_BEDROCK` or standard AWS
  access-key credentials in one Secret.
- Streaming text, command and file activity, direct Streamable HTTP MCP, and
  native Shared agents.
- Standalone and plugin-selected skills without plugin hooks, commands,
  executables, or implicit plugin MCP servers.
- Exact native thread resume and bounded cancellation through `turn/interrupt`.

The adapter deliberately fixes native approvals to `never` and the native
sandbox to `danger-full-access`; the Substrate Actor remains the security
boundary. Account login, API-key passthrough, HITL, custom TLS, legacy SSE MCP,
Dedicated agents, checkpoint/fork guarantees, and configurable native policy
are not advertised.

Runtime configuration is supplied through `KAGENT_CONFIG_JSON` and
`KAGENT_AGENT_CARD_JSON`. Private A2A is served on port 80 and readiness on
`/readyz` at port 8081.

## Development

Codex driver launches the App Server and communicates with it using JSON-RPC 2.0.
For development, it would often be helpful to generate the schema it uses.
You can run the following command with codex and get it in `schema/` folder.

```text
codex app-server generate-json-schema --out go/harness/codex/protocol/schema
```

For more information, see [https://learn.chatgpt.com/docs/app-server#message-schema](https://learn.chatgpt.com/docs/app-server#message-schema)

## Example

```yaml
apiVersion: kagent.dev/v1alpha3
kind: Harness
metadata:
  name: codex-harness
  namespace: kagent
spec:
  codex: {}
  workload:
    image: ${KAGENT_CODEX_IMAGE_DIGEST}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
  allowedAgentTemplates:
    selector:
      matchLabels:
        kagent.dev/e2e-runtime: codex
---
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  labels:
    kagent.dev/e2e-runtime: codex
  name: kagent-codex
  namespace: kagent
spec:
  description: test
  modelConfig:
    name: default-model-config # This modelconfig must have openAI.apiFormat set to "responses"
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
