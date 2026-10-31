# Declarative Workflow Agents

The Go ADK builds agents from an `adk.declarative/v0.2` `AgentSystem` document
with `go/adk/pkg/declarative`. The package compiles a document into native ADK
2.x agents; it adds no execution engine. Routing, fan-out and join, retries,
timeouts, pause and resume, telemetry, and A2A streaming are the behavior of the
ADK `workflow` package and the ADK agent runtime.

This is a library for Go ADK programs, such as a BYO Harness. It is not a session
or task model: A2A still owns interaction state, and a paused workflow resumes
through the same A2A task as any other ADK agent. A BYO image can embed its
document, or receive one through `AgentTemplate.spec.workflowFrom`; see
[Control plane](#control-plane).

## Document model

A document lists agents; the first is the root. Every agent has an
`agent_class`:

| Class             | ADK type                       | Required                 |
| ----------------- | ------------------------------ | ------------------------ |
| `Workflow`        | `workflowagent` graph          | `nodes`; `max_concurrency` caps it |
| `LlmAgent`        | `llmagent`                     | `model`, `instruction`   |
| `SequentialAgent` | `workflowagents/sequentialagent` | `sub_agents`           |
| `ParallelAgent`   | `workflowagents/parallelagent` | `sub_agents`             |
| `LoopAgent`       | `workflowagents/loopagent`     | `sub_agents`; `max_iterations` bounds it |

`sub_agents` names other agents of the document. ADK agent names are unique
within an agent tree, so an agent has at most one parent and `sub_agents` never
cycle. An agent may additionally be the target of any number of `agent` nodes
in different graphs; it is built once and shared. Agents that the root does not
reach are still built and are addressable through `System.Agent` and
`System.Loader`.

Fields that a class does not accept are rejected rather than ignored.

### LLM options

`LlmAgent` agents and inline `llm` nodes share `model`, `mode`, `instruction`,
`global_instruction`, `tools`, `output_key`, `output_schema` (a JSON Schema,
converted with the same code as structured output) and `include_contents`
(`default` or `none`). `LlmAgent` additionally takes
`disallow_transfer_to_parent` and `disallow_transfer_to_peers`.

### Node types

| Type              | Required                   | Behavior                                                        |
| ----------------- | -------------------------- | --------------------------------------------------------------- |
| `function`        | `function`                 | Runs a registered `Func`; may select routes                     |
| `join`            |                            | Fan-in barrier; emits a map keyed by predecessor                |
| `tool`            | `tool`                     | Runs a registered tool with the predecessor output as arguments |
| `agent`           | `agent`                    | Runs an agent of the document                                   |
| `llm`             | `model`, `instruction`     | Inline single-turn agent named after the node id                |
| `workflow`        | `nodes`, `edges`           | Nested graph with its own `START`, nested at most 8 deep        |
| `parallel_worker` | one of function/tool/agent | Runs the target once per item of a list input                   |
| `dynamic`         | `function`                 | Registered `DynamicFunc` that runs children under `max_iters`, with `isolation` |
| `hitl`            | `message`                  | Pauses for a human reply and forwards the reply                 |
| `cel`             | one of output/route/routes/state_delta | Evaluates CEL expressions with `vars`; may select routes and write state |

`function`, `tool`, `agent`, `llm`, `parallel_worker`, `dynamic` and `cel` nodes accept
`timeout` and `retry`, which map to `workflow.NodeConfig`. Unset retry fields
take the ADK defaults. `retry.when` is a CEL predicate over `error`
(`message`, `timeout`, `canceled`) that decides whether a failure is retried,
for example `error.timeout || error.message.contains("transient")`; it maps to
`RetryConfig.ShouldRetry`, which has no context, so state and session are not
visible there. An evaluation error means no retry.

A `dynamic` node decides at run time which children to run. `children` lists
nodes of the same graph that only that node schedules: they carry no edges, and
the function runs one with `DynamicEnv.RunChild(id, input)`. Every child run,
and every `DynamicEnv.Run`, spends one `max_iters` iteration and fails with
`ErrIterationBudget` when none is left.

Edges connect node ids. `START` is the entry source; `entry` is shorthand for a
`START` edge. An edge may carry a `route`, matched against the routes a
`function` or `cel` node returns; `DEFAULT` is the edge taken when no route matched.
There is no `END`: as in ADK, whose only sentinel is `START`, a node without
outgoing edges ends its branch, so a separate finish node is needed only where
the terminal rules below require one.
Unknown fields anywhere in the document are decode errors.

### CEL

A `cel` node evaluates expressions instead of calling Go. `output` replaces
the node output, which otherwise passes the input through; `route` returns a
route name or a list of names; `state_delta` writes each key's value to session
state. All of them see the state as it was before the node ran.

`vars` names intermediate values, like the `variables` of a Kubernetes
ValidatingAdmissionPolicy. Each `{name, expr}` is evaluated in order and read
as `vars.<name>` by later vars and by the other fields. `routes` is the
rule-table alternative to `route`: an ordered list of `{route, when}`; the
first rule whose `when` is true selects its route, an omitted `when` (last rule
only) always matches, and when nothing matches the `DEFAULT` edge is taken.
`route` and `routes` are mutually exclusive. Validation rejects a `vars.<name>`
reference to an undeclared var or, inside `vars`, to a later one.

```yaml
- id: classify
  type: cel
  vars:
    - name: text
      expr: input.lowerAscii()
  routes:
    - route: BUG
      when: vars.text.matches(r'\b(crash|error|bug)\b')
    - route: BILLING
      when: vars.text.matches(r'\b(invoice|refund)\b')
```

Expressions see `input`, `vars` and the ADK context: `state`, `session` (`id`,
`app_name`, `user_id`), `user` (`id`, `content`, the text that started the
invocation) and `invocation` (`id`, `branch`, `agent_name`). Besides the CEL
standard library, including RE2 `matches`, the environment enables the cel-go
extensions for optional types, `cel.bind`, strings (`trim`, `lowerAscii`,
`split`, `format`, ...), base64 encoders, lists, `math`, sets and regex
(`regex.extract`, `regex.extractAll`, `regex.replace`). An empty optional
result is null. Expressions compile at validation and run under a cost limit
of 1,000,000. `go/adk/examples/declarative/agents.yaml` classifies requests
with an ordered list of regular-expression rules.

The helper functions follow the names and semantics of the
[agentgateway CEL functions](https://agentgateway.dev/docs/standalone/latest/reference/cel/variables/),
so one expression reads the same in kagent and agentgateway:

| Helper | Purpose |
| --- | --- |
| `json(text)` | Parse a JSON string. |
| `toJson(value)` | Encode a value as a JSON string. |
| `x.with(name, body)` | Bind `x` to `name` inside `body`; same as `cel.bind(name, x, body)`. |
| `default(a.b[k], fallback)` | The field or index value, or `fallback` when a key on the path is missing. |

Callback rules and `retry.when` use the same environment with the variables of
their scope, described below.

### Prompt sources

`instruction_from` and `global_instruction_from` replace `instruction` and
`global_instruction` with a `TextSource`: exactly one of `config_map` or
`secret` (`{name, key}` in the document's namespace) or `env`. The controller
inlines ConfigMap text before delivery, records the ConfigMap in the revision
provenance, and retries while it is missing. It rejects `secret` and `env`
sources as `UnsupportedConfiguration`: kagent never delivers Secret values to a
runtime (credentials are injected at the egress gateway), so prompt text the
runtime must read belongs in a ConfigMap. A document built directly with the Go
library resolves only `env`; `declarative.ResolvePrompts` lets an embedding
program resolve the other kinds.

### Remote MCP servers

`mcp_servers` declares streamable HTTP MCP servers: `name`, exactly one of
`url` or `url_from` (`{env: VAR}`), optional `headers` (`value` or
`value_from: {env: VAR}`), and an optional `tools` allowlist. Both sourced
fields read from the BYO runtime process environment. A server is used two ways:

- a `tool` node with `server:` calls one of its tools as a function node; the
  predecessor output is the arguments and the node output is the tool result,
  `{"output": ...}` for both structured and text results;
- an `LlmAgent` or `llm` node lists the server name in `tools`, giving the
  model the server's allowlisted tools.

Connecting creates the ADK `mcptoolset` without contacting the server: tools
are listed when first needed, so an unreachable server fails a run, not the
build, and a node retries per its `retry` policy. `System.Close` closes every
MCP session. `Registry.SetMCPConnector` replaces the default connector, for
example to add per-request credentials.

### Callbacks

`callbacks` on an `LlmAgent` or `llm` node maps to the ADK before/after model
and tool callbacks: `before_model`, `after_model`, `before_tool`, `after_tool`.
Each is an ordered list of rules. A rule applies when its CEL `when` is true
(empty always applies) and either decides declaratively with `action: accept`
(run the call unchanged and stop evaluating) or `action: reject` with a
`message`, or names a registered `Hook` with `function`. A rejected model call
is answered with the message; a rejected tool call returns `{"error": message}`
to the model. Both carry `kagent_hook` metadata so clients can tell them from
real output. Rules see the context variables plus `request` (before_model:
`model`, `text`, `contents`, `system_instruction`, `tools`), `response`
(after_model: `text`, `partial`, `turn_complete`, `error`, `finish_reason`,
`function_calls`) or `tool` (`name`, `args`, and after the call `result` and
`error`). For example, a `before_tool` rule with
`when: tool.name == 'lookup_customer' && tool.args.customer_id != state.customer_id`
and `action: reject` keeps a model from reading another customer's record.

### Harness tools

`Registry.RegisterHarnessTools(HarnessConfig{Workspace, DisableShell})`
registers the local tool library: `read_file`, `write_file`, `list_files` and
`bash`, the same implementations the kagent runtime uses. Relative paths
resolve against the workspace; isolation comes from the sandbox the runtime
runs in. They are opt-in, because a document's image decides what it exposes,
and can be `tool` nodes (CEL builds their arguments) or LLM tools.

### Typed contracts

A `function`, `dynamic`, `agent` or `llm` node can carry an `input_schema`, and a
`function` or `dynamic` node an `output_schema` (an `llm` node's `output_schema`
is the model's reply schema). On a `join`, `input_schema` applies to each
predecessor's output separately. The engine validates what a node returns and
what the next node receives, and compares the two schemas on every edge when it
builds the graph, so a producer that promises a string cannot feed a consumer
that requires an integer. Validation parses and resolves each schema, so a
malformed one fails at reconcile time.

### Output visibility

A2A clients display event message text only; successor nodes read the structured
`Event.Output`. Function nodes are plain ADK function nodes: they emit data, so
intermediate steps stay out of the chat, exactly as with a native
`workflow.NewFunctionNode`. For the terminal nodes of a graph, `function`,
`dynamic` and `hitl` nodes also render their output as message text: strings as
they are, structured values as JSON. A terminal `tool`, `join` or
`parallel_worker` node would end the run with output no client displays, so
validation rejects it: end such a branch with a `function` node. A nested
`workflow` node is checked through its own terminals.

### Session isolation

How much of the session a concurrent branch or a subagent sees is a choice with
four levels: shared, branch, scope, and a separate session. Static fan-out,
`parallel_worker` items and `ParallelAgent` sub-agents are always isolated by
branch. Two fields control the rest:

- `isolation` on a `dynamic` node sets how its children run: `shared` (the
  default, the whole conversation), `branch` (siblings do not see each other)
  or `scope` (a child sees only its own events).
- `agent_tools` on an `LlmAgent` calls other agents as tools. Each call runs in a
  new session seeded with a copy of the caller's state, so the agent never sees
  the caller's history, parallel calls cannot interfere, and its state writes
  are not copied back. An agent is either a `sub_agents` entry (shared session,
  transfer) or an `agent_tools` entry, never both. This is the declarative form
  of `isolation: Isolated` on an AgentTemplate subagent binding, which the
  controller delivers to the kagent and BYO runtimes
  ([Session isolation](a2a-subagents.md#session-isolation)).

Branches isolate history, not state. Validation rejects two agents that run
concurrently and write the same `output_key`: `ParallelAgent` sub-agents,
unconditional fan-out successors, and a `parallel_worker` agent with an
`output_key`. Cycles through `sub_agents`, `agent_tools` and `agent` nodes are
rejected too, since each would build forever.

[Workflow design patterns](workflow-patterns.md#session-isolation) has the full
matrix, and runnable patterns that print what each agent saw. The behavior is
pinned by `isolation_test.go`, which measures it rather than assuming it.

## Boundaries

| Concern                                                  | Owner                           |
| -------------------------------------------------------- | ------------------------------- |
| Document shape, per-class and per-type field rules, name references between agents and nodes, output visibility | `Validate` (pure, no registry) |
| Names that resolve to Go values (functions, tools, models) | `Registry.Validate`           |
| Reachability, unconditional cycles, fan-in, task mode    | ADK workflow engine, at `Build` |
| Execution, routing, retries, pause and resume            | ADK workflow engine             |

Validation reports every issue with a field path such as
`agents[0].nodes[2].function`, including unregistered names. `Registry.Parse`
and `Build` run it; a model resolver can only fail at `Build`, because only it
knows whether a name resolves. Errors from the ADK engine are wrapped with the
agent and node they belong to and are not `ValidationError`s.

A document carries no code. `Registry` attaches behavior by name:
`RegisterFunction`, `RegisterDynamic`, `RegisterTool`, `RegisterToolset`,
`RegisterModel`, and `SetModelResolver` for model names that are not registered
by value (for example to reuse the runtime's provider factories). The offline
`stub` model and the ADK memory tools are preregistered. Tools and toolsets are
registered under their own `Name()`. Toolsets, such as MCP, can be listed in an
LLM `tools` list but cannot back a `tool` node.

The registry is populated before `Build` and read-only afterwards. Documents are
authored by the agent's owner and are trusted like code that selects registered
behavior; validation bounds nesting depth but not document size.

## Extending

`Registry.RegisterNodeType` adds a node type: a `NodeRule` listing the
`NodeSpec` fields it requires and accepts, and a `NodeBuilder` returning any
`workflow.Node`. Documents using custom types are parsed with `Registry.Parse`
so validation applies the same rules. Built-in types cannot be replaced.
Agent-level customization that ADK exposes as Go callbacks stays in Go: build
the document, then wrap or compose the resulting agents.

## Constraints inherited from the ADK engine

- An unset `mode` is `single_turn` for an agent placed as a graph node and a
  declared `chat` mode is honored there, keeping history. `task` agents cannot be
  graph nodes; `Build` surfaces the engine's rejection. Inline `llm` nodes are
  always `single_turn` and validation rejects other modes.
- Mutually exclusive routes must not fan in to a non-`join` node; give each
  route its own terminal. A `join` suits fan-out that always runs every branch.
- An `agent` node is named after its agent inside the engine, so two nodes of
  one graph cannot wrap the same agent and an agent name cannot equal a node id.
- Fan-out from edges starts every successor at once unless the Workflow agent
  sets `max_concurrency`. The cap is graph-wide: at most that many nodes of the
  graph run at once and further ready nodes wait. A nested `workflow` node and
  the items of a `parallel_worker` run inside their parent node and are not
  counted separately; `parallel_worker.max_concurrency` bounds the items of one
  worker. `workflowagent.Config` does not accept engine options, so a capped
  Workflow is built with `workflow.WithMaxConcurrency` behind a small agent that
  runs or resumes the graph; its telemetry span is `invoke_agent`, not
  `invoke_workflow`.
- A `parallel_worker` target runs without routes; a `Func` result's routes are
  ignored there.
- A `hitl` node re-runs on resume. Its interrupt id combines the node id and the
  invocation id, so it is stable within a run and unique across runs.

## Control plane

`AgentTemplate.spec.workflowFrom` references a document, as YAML or JSON, in a
same-namespace ConfigMap key, like `systemPromptFrom` and `outputSchemaFrom`.
The controller:

1. reads the key and rejects a document larger than 64 KiB;
2. decodes it strictly and runs `declarative.Validate`, the registry-free
   structural checks above, so a typo fails at reconcile time with its field
   path instead of crash-looping the runtime;
3. canonicalizes it to JSON, records its SHA-256, and delivers both in the BYO
   runtime's `config.json` as `agent.workflow`;
4. records the ConfigMap in the revision provenance, so editing the document
   prepares a new revision.

Failures that are the document's fault report `Compatible=False` with reason
`UnsupportedConfiguration` and the validation message. A missing ConfigMap or key
is an ordinary reference failure and is retried.

What the controller cannot check is whether the image has the functions, tools
and models the document names. Those live in the image, so the runtime checks
them at startup with `Registry.ParseConfig`, which also verifies the delivered
digest. A document therefore pairs with the image that registers its names; roll
them out together.

Constraints, enforced at compile time:

- Only a BYO Harness runs a workflow. The managed kagent runtime has no
  function registry, so it rejects the config as well as the controller;
  Codex and Claude cannot run ADK graphs.
- Only the root template may set `workflowFrom`. A shared subagent template that
  sets it is rejected rather than ignored.
- `workflowFrom` cannot be combined with `systemPrompt` or `systemPromptFrom`;
  the CRD enforces this with CEL, along with non-empty `name` and `key`.

The CRD does not model the document. CRD schemas cannot be recursive, so nested
`workflow` nodes cannot be typed, and a second copy of the rules in CEL would
drift from the Go validation.

## Examples and tests

`go/adk/examples/declarative` serves a document through the A2A application
builder: CEL classifies and routes requests, a remote CRM MCP server answers
billing questions through a tool node and an LLM node guarded by a CEL tool
callback, and harness `write_file` stores escalation tickets.
[Workflow design patterns](workflow-patterns.md) shows router, fan-out and fan-in,
map-reduce, loops, human approval, and more, each as Go next to YAML, with the
graphs the engine rejects. `go/adk/pkg/declarative/testdata` holds two documents the package tests run end
to end: `showcase.yaml` exercises every node type, route form and LLM option, and
`composites.yaml` the sequential, parallel and loop agents.
