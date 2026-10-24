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
| `Workflow`        | `workflowagent` graph          | `nodes`                  |
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

`function`, `tool`, `agent`, `llm`, `parallel_worker` and `dynamic` nodes accept
`timeout` and `retry`, which map to `workflow.NodeConfig`. Unset retry fields
take the ADK defaults.

A `dynamic` node decides at run time which children to run. `children` lists
nodes of the same graph that only that node schedules: they carry no edges, and
the function runs one with `DynamicEnv.RunChild(id, input)`. Every child run,
and every `DynamicEnv.Run`, spends one `max_iters` iteration and fails with
`ErrIterationBudget` when none is left.

Edges connect node ids. `START` is the entry source; `entry` is shorthand for a
`START` edge. An edge may carry a `route`, matched against the routes a
`function` node returns; `DEFAULT` is the edge taken when no route matched.
Unknown fields anywhere in the document are decode errors.

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
- `workflowagent.Config` exposes no engine options, so a concurrency cap for the
  whole graph is not available; `parallel_worker.max_concurrency` bounds one
  worker.
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
builder, with Go functions, a tool, and an OpenAI model resolver registered.
[Workflow design patterns](workflow-patterns.md) shows router, fan-out and fan-in,
map-reduce, loops, human approval, and more, each as Go next to YAML, with the
graphs the engine rejects. `go/adk/pkg/declarative/testdata` holds two documents the package tests run end
to end: `showcase.yaml` exercises every node type, route form and LLM option, and
`composites.yaml` the sequential, parallel and loop agents.
