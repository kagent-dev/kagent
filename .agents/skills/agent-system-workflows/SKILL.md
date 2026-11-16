---
name: agent-system-workflows
description: >
  Use when authoring, reviewing or debugging an adk.declarative/v0.2 AgentSystem
  document (agent-system.yaml) and its kagent delivery: a Workflow graph of
  nodes and edges, LlmAgent / SequentialAgent / ParallelAgent / LoopAgent,
  cel / llm / tool / agent / join / parallel_worker / dynamic / hitl nodes,
  CEL routing, callbacks, remote MCP servers, harness tools, subagent isolation,
  AgentTemplate.spec.workflowFrom, or the generic workflow-agent BYO image.
---

# AgentSystem workflow documents

An `AgentSystem` document (`apiVersion: adk.declarative/v0.2`) declares ADK Go
2.x agents and workflow graphs as YAML, with no Go routing code. The Go package
[`go/adk/pkg/declarative`](../../../go/adk/pkg/declarative) compiles it into
native ADK agents; it adds no engine. In kagent, an `AgentTemplate` delivers the
document through `spec.workflowFrom`, and the generic **workflow-agent** BYO
image serves it.

**Sources of truth** (read these for anything this skill does not cover):
[docs/architecture/workflow-agents.md](../../../docs/architecture/workflow-agents.md)
(the format reference),
[docs/architecture/workflow-patterns.md](../../../docs/architecture/workflow-patterns.md)
(22 patterns, each Go next to YAML, plus the graphs the engine rejects), and the
Go types in [`spec.go`](../../../go/adk/pkg/declarative/spec.go).

## Always validate before claiming a document is correct

A document is **not** done until it passes validation. The schema is strict:
unknown fields anywhere are decode errors, and per-class / per-type field rules
reject fields that do not apply rather than ignoring them. Run the bundled
checker, which applies the controller's checks, the runtime registry, and the
ADK engine's graph rules, offline and without credentials:

```sh
cd go
go run ../.agents/skills/agent-system-workflows/scripts/validate.go path/to/agent-system.yaml
# run each branch once on the offline stub model — the only check that catches a
# wrong field access on an untyped node input (e.g. a tool node's output map):
go run ../.agents/skills/agent-system-workflows/scripts/validate.go -input "refund please" -reply yes path/to/agent-system.yaml
```

It reports issues with a field path such as `agents[0].nodes[2].function`. A
document that only names registered-by-the-image functions/tools is fine to
report `is not registered` for those names — that check belongs to the image
(see [Delivery](#delivery)). Everything else must be clean.

Do not hand-wave validity from the YAML looking reasonable. The
[rejected graphs](../../../docs/architecture/workflow-patterns.md#rejected-graphs)
all look reasonable and fail.

## The document

The first agent is the root. Every agent has an `agent_class`:

| Class | Builds | Needs |
| --- | --- | --- |
| `Workflow` | a node/edge graph | `nodes`; `max_concurrency` caps the whole graph |
| `LlmAgent` | one model-backed agent | `model`, `instruction` |
| `SequentialAgent` | run sub-agents in order | `sub_agents` |
| `ParallelAgent` | run sub-agents concurrently (branch-isolated) | `sub_agents` |
| `LoopAgent` | repeat sub-agents until escalate / `max_iterations` | `sub_agents` |

### Node types (inside a `Workflow`)

| `type` | Needs | Does |
| --- | --- | --- |
| `cel` | one of `output`/`route`/`routes`/`state_delta` | evaluate CEL; choose routes; write state — no Go |
| `function` | `function` | run a registered `Func`; may return routes |
| `tool` | `tool` (`server` for MCP/toolset) | run a tool with the predecessor output as arguments |
| `llm` | `model`, `instruction` | inline single-turn agent named after the node id |
| `agent` | `agent` | run another agent of the document |
| `join` | — | fan-in barrier; emits a map keyed by predecessor |
| `parallel_worker` | one of `function`/`tool`/`agent` | run the target once per list item (`max_concurrency`) |
| `dynamic` | `function` | Go schedules `children` under `max_iters`, with `isolation` |
| `workflow` | `nodes`, `edges` | nested graph with its own `START`, max depth 8 |
| `hitl` | `message` | pause for a human reply, forward it |

Edges connect node ids. `START` is the only sentinel (`entry: NODE` is shorthand
for a `START` edge); there is **no `END`** — a node with no outgoing edge ends
its branch. An edge may carry a `route`; `DEFAULT` is taken when no route
matched. `timeout` and `retry` (with a CEL `retry.when` over `error`) apply to
node types that run user code.

### CEL nodes replace routing code

A `cel` node is the declarative workhorse: classify, route, and shape data with
no Go. `vars` are ordered named values read as `vars.<name>`; `routes` is an
ordered first-match rule table (`{route, when}`); `output` replaces the passthrough
output; `state_delta` writes session state. Expressions see `input`, `vars`,
`state`, `session`, `user`, `invocation`, the cel-go standard library and the
strings/regex/lists/math/sets/base64/optionals extensions, plus the helpers
`json`, `toJson`, `x.with(name, body)` and `default(a.b, fallback)`. Every
expression is type-checked at validation.

```yaml
- id: classify
  type: cel
  vars:
    - { name: text, expr: user.content.lowerAscii() }
  routes:                       # first match wins; no match -> DEFAULT edge
    - { route: BILLING, when: "vars.text.matches(r'\\b(invoice|refund|charge)\\b')" }
    - { route: BUG,     when: "vars.text.matches(r'\\b(crash|error|bug)\\b')" }
```

### Everything else

- **Prompts:** `instruction_from` / `global_instruction_from` read a `config_map`
  key. **Secret and env prompt sources are rejected by the controller** — kagent
  never ships Secret values to a runtime; put prompt text in a ConfigMap.
- **Remote MCP:** declare servers under `mcp_servers` (`url` or `url_from: {env}`,
  optional `headers`, optional `tools` allowlist). A `tool` node with `server:`
  calls one tool (predecessor output is the arguments); its **output is
  `{"output": ...}`** — read it as `input.output`, not `string(input)`. An
  `llm`/`LlmAgent` listing the server name in `tools` offers its tools to the model.
- **Harness tools:** `read_file`, `write_file`, `list_files`, `bash`, always
  registered by the generic image — a document uses one by naming it as a `tool`
  node's `tool:` or an LLM tool; nothing else is needed to enable them. A `tool`
  node's **output is a map, not the raw string:** builtin tools yield
  `{"result": ...}` (read `input.result`). `write_file` takes `{file_path, content}`,
  `read_file` takes `{file_path}`. Because node inputs are untyped, a wrong field
  access passes validation and only fails under a stub run — so run `-input` on any
  branch that threads a `tool` node output into a later `cel` node.
- **Callbacks:** `before_model` / `after_model` / `before_tool` / `after_tool`
  are ordered rules — a CEL `when` plus `action: accept|reject` (+`message`) or a
  registered `function` hook — for guardrails.
- **Typed contracts:** `input_schema` / `output_schema` (JSON Schema) are compared
  across every edge at build time.

## Session isolation

Concurrency isolates **history, not state**. Static fan-out, `parallel_worker`
items, and `ParallelAgent` sub-agents are always branch-isolated. `dynamic`
children take `isolation: shared | branch | scope`. `sub_agents` of an `LlmAgent`
share the session (transfer); list an agent under `agent_tools` instead to call
it as a tool in a fresh session seeded with a copy of state. The controller's CRD
analogue is a subagent binding's `isolation: Shared | Isolated`. Validation
rejects two concurrent writers of the same `output_key`. See the
[isolation matrix](../../../docs/architecture/workflow-patterns.md#session-isolation).

## Delivery

`AgentTemplate.spec.workflowFrom` names a same-namespace ConfigMap key holding the
document. On reconcile the controller bounds it to 64 KiB, decodes it strictly,
runs `declarative.Validate`, inlines ConfigMap prompts (recording them in
provenance so an edit rolls a new revision), canonicalizes to JSON, and delivers
it with its SHA-256. Constraints, all compile-time:

- **Only a BYO Harness runs a workflow.** Managed kagent, Codex and Claude reject
  it; they have no function registry.
- **Only the root template** may set `workflowFrom`.
- `workflowFrom` **excludes** `systemPrompt` / `systemPromptFrom` (CRD CEL rule).

The controller cannot check names the image registers (functions, tools, models);
the runtime verifies them at startup and checks the delivered digest. **Roll the
document and its image together.**

The generic **workflow-agent** image
([`go/adk/examples/declarative`](../../../go/adk/examples/declarative),
`make build-workflow-agent`) carries no workflow code: it resolves every model
name to the AgentTemplate's ModelConfig and offers the harness tools, so a
document only names `cel`/`tool`/`agent`/`llm` behavior it already has.
[`go/adk/examples/workflow`](../../../go/adk/examples/workflow) has complete,
deployable `agent-system.yaml` + `manifests.yaml` + `deploy.sh` sets
(writer-critics, llm-router) — copy one as a starting manifest.

## Minimal deployable example

```yaml
# agent-system.yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: triage
    agent_class: Workflow
    nodes:
      - id: classify
        type: cel
        vars:
          - { name: text, expr: user.content.lowerAscii() }
        routes:
          - { route: QUESTION, when: "vars.text.endsWith('?')" }
      - { id: answer, type: llm, model: default, instruction: "Answer the user." }
      - { id: ack, type: cel, output: "'Thanks, logged: ' + user.content" }
    edges:
      - { from: START, to: classify }
      - { from: classify, to: answer, route: QUESTION }
      - { from: classify, to: ack, route: DEFAULT }
```

```yaml
# manifests.yaml — the ConfigMap is built from the document:
#   kubectl create configmap triage-workflow --from-file=agent-system.yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Harness
metadata: { name: triage, namespace: kagent }
spec:
  byo: {}
  workload:
    command: ["/app"]
    image: localhost:5001/kagent-dev/kagent/workflow-agent:dev   # digest-pin for real deploys
  substrate:
    workerPoolRef: { name: kagent-default }
    snapshotPolicy: { location: s3://kagent-snapshots/triage }   # required
---
apiVersion: api.kagent.dev/v1alpha3
kind: AgentTemplate
metadata: { name: triage, namespace: kagent }
spec:
  description: Routes questions to an LLM and logs everything else
  modelConfig: { name: default-model-config }   # resolves the `default` model name
  workflowFrom: { name: triage-workflow, key: agent-system.yaml }
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata: { name: triage, namespace: kagent }
spec:
  harnessRef: { name: triage }
  template: { ref: { name: triage } }
```

## Common mistakes

| Mistake | Fix |
| --- | --- |
| Adding an `END` node or sentinel | Omit it; a node with no outgoing edge ends its branch |
| Mutually exclusive routes converging on a non-`join` node | Give each route its own terminal, or use a `join` |
| An unconditional back-edge (loop) | Route the back-edge (e.g. `route: REVISE`); unconditional cycles are rejected |
| Ending a branch on a `tool`/`join`/`parallel_worker` node | A2A shows no output; end with a `function`/`cel`/`hitl`/`llm` node |
| Reading a `tool` node output as `string(input)` | Its output is a map — `input.output` (MCP) or `input.result` (harness); confirm with a `-input` stub run |
| Two concurrent agents writing the same `output_key` | Rejected; give each its own key or converge them |
| A `task`-mode agent as a static graph node | Only `single_turn`/`chat` nodes; dispatch task agents from a `dynamic` node |
| Node id equal to an `agent` name | An `agent` node is named after its agent; rename one |
| Prompt from a Secret or env | Controller rejects it; inline via a ConfigMap `config_map` source |
| `systemPrompt` alongside `workflowFrom` | Mutually exclusive on the AgentTemplate |
| Expecting managed kagent/Codex/Claude to run it | Only a BYO Harness runs a document |
| Trusting the YAML by eye | Run `scripts/validate.go` |
