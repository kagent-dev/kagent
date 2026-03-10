# Existing Solutions: Declarative Workflows over Durable Execution Engines

**Date:** 2026-03-10
**Status:** Research

---

## Table of Contents

1. [Temporal DSL Sample](#1-temporal-dsl-sample)
2. [Orkes Conductor (Netflix)](#2-orkes-conductor-netflix)
3. [Hatchet](#3-hatchet)
4. [Windmill](#4-windmill)
5. [Other Notable Projects](#5-other-notable-projects)
   - [Serverless Workflow Specification (CNCF)](#51-serverless-workflow-specification-cncf)
   - [Zigflow](#52-zigflow)
   - [Kestra](#53-kestra)
   - [Dagu](#54-dagu)
   - [GraphAI](#55-graphai)
   - [Restate](#56-restate)
6. [Comparative Analysis](#6-comparative-analysis)
7. [Patterns That Worked vs. Failed](#7-patterns-that-worked-vs-failed)

---

## 1. Temporal DSL Sample

**Source:** [temporalio/samples-go/dsl](https://github.com/temporalio/samples-go/tree/main/dsl)

### DSL Format

The official Temporal DSL sample uses YAML to define workflows as a tree of **Statements**. Each Statement is one of three types: `activity`, `sequence`, or `parallel`.

**Simple sequential workflow (workflow1.yaml):**

```yaml
variables:
  arg1: value1
  arg2: value2

root:
  sequence:
    elements:
     - activity:
        name: SampleActivity1
        arguments:
          - arg1
        result: result1
     - activity:
        name: SampleActivity2
        arguments:
          - result1
        result: result2
     - activity:
        name: SampleActivity3
        arguments:
          - arg2
          - result2
        result: result3
```

**Parallel with fan-out/fan-in (workflow2.yaml):**

```yaml
variables:
  arg1: value1
  arg2: value2
  arg3: value3

root:
  sequence:
    elements:
      - activity:
         name: SampleActivity1
         arguments:
           - arg1
         result: result1
      - parallel:
          branches:
            - sequence:
                elements:
                 - activity:
                    name: SampleActivity2
                    arguments:
                      - result1
                    result: result2
                 - activity:
                    name: SampleActivity3
                    arguments:
                      - arg2
                      - result2
                    result: result3
            - sequence:
                elements:
                 - activity:
                    name: SampleActivity4
                    arguments:
                      - result1
                    result: result4
                 - activity:
                    name: SampleActivity5
                    arguments:
                      - arg3
                      - result4
                    result: result5
      - activity:
         name: SampleActivity1
         arguments:
           - result3
           - result5
         result: result6
```

### Execution Model

The Go implementation defines these core types:

```go
type Workflow struct {
    Variables map[string]string   // Initial variable bindings
    Root      Statement           // Entry point
}

type Statement struct {
    Activity *ActivityInvocation  // Leaf node: calls an activity
    Sequence *Sequence            // Sequential execution
    Parallel *Parallel            // Concurrent execution
}

type ActivityInvocation struct {
    Name      string    // Activity function name
    Arguments []string  // Variable names to pass as input
    Result    string    // Variable name to store output
}
```

Key implementation details:
- **Variable passing:** A shared `bindings map[string]string` is threaded through all executions. Activities read arguments from it and write results back into it.
- **Parallelism:** Uses `workflow.Go()` (Temporal coroutines) to launch branches concurrently. A `workflow.Selector` waits for all branches. If any branch fails, all others are cancelled via `workflow.WithCancel`.
- **Error handling:** Errors bubble up. A failed activity cancels sibling parallel branches. No retry configuration in the DSL itself (relies on Temporal's activity options set in Go code).

### Assessment

| Aspect | Detail |
|--------|--------|
| **Strengths** | Minimal, easy to understand; shows the core pattern cleanly; fully deterministic via Temporal replay |
| **Weaknesses** | No conditionals, no loops, no retry config in YAML; variables are string-only; activity options hardcoded in Go; designed as a teaching sample, not production-ready |
| **Abstraction level** | Very low -- thin wrapper over Temporal primitives |

---

## 2. Orkes Conductor (Netflix)

**Source:** [Orkes Conductor Documentation](https://orkes.io/content/developer-guides/workflows)

### DSL Format

Conductor workflows are defined as **JSON documents** (or via SDKs that generate JSON). The core structure:

```json
{
  "name": "order_processing",
  "description": "Process customer orders",
  "version": 1,
  "schemaVersion": 2,
  "tasks": [
    {
      "name": "validate_order",
      "taskReferenceName": "validate_ref",
      "type": "SIMPLE",
      "inputParameters": {
        "orderId": "${workflow.input.orderId}"
      }
    },
    {
      "name": "fork_processing",
      "type": "FORK_JOIN",
      "forkTasks": [
        [
          {"name": "check_inventory", "taskReferenceName": "inv_ref", "type": "SIMPLE"}
        ],
        [
          {"name": "charge_payment", "taskReferenceName": "pay_ref", "type": "SIMPLE"}
        ]
      ]
    },
    {
      "name": "join_processing",
      "type": "JOIN",
      "joinOn": ["inv_ref", "pay_ref"]
    }
  ],
  "outputParameters": {
    "result": "${join_processing.output}"
  },
  "failureWorkflow": "order_failure_handler",
  "timeoutPolicy": "ALERT_ONLY",
  "timeoutSeconds": 3600
}
```

### Execution Model

- Conductor server stores workflow definitions and manages execution state.
- Workers poll for tasks, execute them, and report results back.
- The server is the single source of truth for workflow state.

### Parallelism / DAGs

- **FORK_JOIN**: Static parallel branches defined at design time.
- **DYNAMIC_FORK**: Runtime-determined parallel branches based on input data.
- **JOIN**: Merges parallel branches back together.
- **SUB_WORKFLOW**: Compose workflows within workflows.

### Variable Passing

Uses JSONPath-like expression syntax:
- `${workflow.input.key}` -- workflow input
- `${taskReferenceName.output.key}` -- output from a specific task
- `${workflow.variables.key}` -- workflow-level variables (set via SET_VARIABLE task)
- `${workflow.secrets.key}` -- secrets
- Nested access: `${ref.output.nested.deep.value}`
- Variables are scoped to a single workflow instance (not shared across sub-workflows).

### Error Handling / Retries

- Per-task retry configuration (count, delay, backoff).
- `failureWorkflow` at the workflow level triggers a compensation workflow.
- `timeoutPolicy` with configurable seconds.
- `rateLimitConfig` for concurrency control.
- Tasks can define `TIMEOUT`, `RETRY`, and `TIME_OUT_WF` policies.

### Operators (Control Flow)

All executed by the engine, no external workers needed:
- **Switch**: Branch based on conditions (like switch-case).
- **Do-While**: Loop until condition is false.
- **Fork/Join**: Static parallel execution.
- **Dynamic Fork**: Runtime-determined parallel branches.
- **Sub Workflow / Start Workflow**: Composition and async invocation.
- **Set Variable**: Mutate workflow-level variables.
- **Terminate**: End workflow with a specific status.

### Assessment

| Aspect | Detail |
|--------|--------|
| **Strengths** | Battle-tested at Netflix scale; rich operator set; visual UI for workflow design; built-in versioning; strong variable passing with JSONPath |
| **Weaknesses** | JSON is verbose for complex workflows; no code-level flexibility within the DSL; task worker model adds latency (polling); vendor lock-in concerns with Orkes Cloud |
| **Abstraction level** | High -- full workflow orchestration with built-in control flow |

---

## 3. Hatchet

**Source:** [Hatchet Documentation](https://docs.hatchet.run/home/dags)

### DSL Format

Hatchet uses a **code-first declarative** approach. Workflows are defined in Python or TypeScript using decorators/builders, not YAML/JSON. However, the design is inherently declarative: you declare tasks and their dependencies, and the engine handles execution order.

**Python example:**

```python
from hatchet_sdk import Context, EmptyModel, Hatchet
from datetime import timedelta

hatchet = Hatchet()
dag_workflow = hatchet.workflow(name="DAGWorkflow")

@dag_workflow.task(execution_timeout=timedelta(seconds=5))
def step1(input: EmptyModel, ctx: Context) -> StepOutput:
    return StepOutput(random_number=random.randint(1, 100))

@dag_workflow.task(execution_timeout=timedelta(seconds=5))
def step2(input: EmptyModel, ctx: Context) -> StepOutput:
    return StepOutput(random_number=random.randint(1, 100))

@dag_workflow.task(parents=[step1, step2])
async def step3(input: EmptyModel, ctx: Context) -> RandomSum:
    one = ctx.task_output(step1).random_number
    two = ctx.task_output(step2).random_number
    return RandomSum(sum=one + two)
```

### Execution Model

Hatchet supports two complementary patterns:

1. **DAGs (Declarative):** Shape of work is known upfront. You declare tasks and dependencies. The engine handles execution order, parallelism, and retries within the fixed structure. Tasks run as soon as their parents complete; independent tasks run in parallel automatically.

2. **Durable Tasks (Imperative):** Shape of work is dynamic. A single long-running function that can pause (`SleepFor`, `WaitForEvent`), spawn child tasks at runtime, and make decisions procedurally. State is checkpointed in the Hatchet event log.

### Parallelism / DAGs

- Implicit parallelism: tasks without dependencies or with satisfied dependencies run concurrently.
- No explicit parallel/fork constructs needed -- the DAG structure itself determines parallelism.
- Worker slots are only allocated when tasks are ready (no wasted resources on waiting).

### Variable Passing

- Parent task outputs are cached and passed downstream.
- Child tasks access parent outputs via `ctx.task_output(parent_task)`.
- Typed: returns the parent's output model for direct property access.
- Completed tasks skip re-execution on mid-workflow failure recovery.

### Error Handling / Retries

- Per-task `execution_timeout` configuration.
- Configurable retry policies per task.
- Dashboard tracks every task execution: inputs, outputs, durations, errors.
- On failure, completed tasks are not re-executed (cached results).

### Assessment

| Aspect | Detail |
|--------|--------|
| **Strengths** | Clean separation of DAG (declarative) vs. durable (imperative); automatic parallelism from dependency graph; type-safe variable passing; good developer experience |
| **Weaknesses** | Not a true DSL/YAML approach -- requires code; relatively new project; no YAML/JSON serialization of workflows for non-developers |
| **Abstraction level** | Medium -- code-first but declaratively structured |

---

## 4. Windmill

**Source:** [Windmill Documentation](https://www.windmill.dev/docs/openflow)

### DSL Format

Windmill uses the **OpenFlow** format, an open JSON-serializable standard for defining flows. Workflows can be authored via:
1. A visual low-code flow editor (primary).
2. YAML/JSON directly.
3. "Workflows as code" in Python/TypeScript.

Each step is a script with a `main()` function in TypeScript, Python, Go, PHP, Bash, or raw SQL.

### Execution Model

- Flows are a linear sequence of **modules** (steps), potentially with branches and loops.
- Each step runs on a worker; the orchestrator manages data flow between steps.
- Steps are isolated scripts -- each has its own dependencies and execution environment.
- The flow editor generates the OpenFlow JSON; execution is handled by the Windmill engine.

### Parallelism / DAGs

- **BranchOne**: Conditional -- run exactly one branch based on predicates (evaluated in order, first match wins, with a default fallback).
- **BranchAll**: Parallel -- run all branches, collect results. Can configure per-branch failure tolerance.
- **For-loops**: Iterate over lists with configurable parallelism (N concurrent iterations).
- Not a full DAG engine -- primarily linear with branching/looping constructs.

### Variable Passing

- **input_transforms**: The piping mechanism from previous steps, variables, or resources to step inputs. Uses JavaScript expressions for evaluation.
- Each step's output is accessible to subsequent steps.
- Flow expressions (input transforms, branch predicates, for-loop iterators) are evaluated using a JavaScript engine.

### Error Handling / Retries

- **Retries**: Per-step configurable retry count.
- **Error handlers**: Special flow step executed when an error occurs; receives the error result as input.
- **Early stop/break**: Stop the flow on specific conditions.
- **Continue on error**: Mark steps as `allowFailure`; flow continues with a WARNING state.
- **Custom timeouts**: Per-step timeout configuration.

### Assessment

| Aspect | Detail |
|--------|--------|
| **Strengths** | Rich visual editor; polyglot steps (any language); OpenFlow is an open format; good balance of low-code and code flexibility |
| **Weaknesses** | Not a durable execution engine (no replay/event sourcing); linear flow model limits complex DAG patterns; each step is a full script (heavy for simple transforms) |
| **Abstraction level** | High for simple flows; medium for complex logic |

---

## 5. Other Notable Projects

### 5.1 Serverless Workflow Specification (CNCF)

**Source:** [serverlessworkflow.io](https://serverlessworkflow.io/) | [GitHub](https://github.com/serverlessworkflow/specification)

A **vendor-neutral, CNCF sandbox** specification for defining workflows in YAML or JSON. Currently at version 1.0.0.

**Example:**

```yaml
id: greeting
version: '1.0.0'
specVersion: '0.8'
name: Greeting Workflow
start: Greet
functions:
- name: greetingFunction
  operation: file://myapis/greetingapis.json#greeting
states:
- name: Greet
  type: operation
  actions:
  - functionRef:
      refName: greetingFunction
      arguments:
        name: "${ .person.name }"
  end: true
```

**Key characteristics:**
- State-machine model with typed states (operation, event, switch, parallel, forEach, inject, sleep).
- Functions map to external service invocations (HTTP, gRPC, OpenAPI, AsyncAPI).
- JQ-based expressions for data filtering and transformation.
- Built-in retry, timeout, and error handling policies.
- Parallel state for concurrent branch execution.
- Event-driven: can wait for and emit CloudEvents.
- **Multiple runtime implementations** exist (SonataFlow/KIE, Synapse, etc.).
- Zigflow bridges this spec to Temporal (see below).

### 5.2 Zigflow

**Source:** [zigflow.dev](https://zigflow.dev/)

A **Temporal-specific implementation** of the Serverless Workflow DSL.

```yaml
document:
  dsl: 1.0.0
  namespace: zigflow
  name: query
  version: 0.0.1
  title: Query Listeners
do:
  - queryState:
      listen:
        to: one
        with:
          id: get_state
          type: query
          data:
            id: ${ $data.id }
            status: ${ $data.status }
  - createState:
      set:
        id: ${ uuid }
        status: not started
  - wait:
      wait:
        seconds: 5
  - updateState:
      set:
        progressPercentage: 33
        status: running
```

**Key characteristics:**
- YAML workflows compile to Temporal workflow executions at runtime.
- Serverless Workflow functions map to Temporal Activity invocations.
- Supports query listeners, signals, timers.
- `zigflow run -f workflow.yaml` starts a worker and registers the compiled workflow.
- Aims to be the bridge between CNCF Serverless Workflow spec and Temporal's execution engine.

### 5.3 Kestra

**Source:** [kestra.io](https://kestra.io/docs/workflow-components/flow)

A **declarative, YAML-first** workflow orchestration platform. Event-driven.

```yaml
id: etl-pipeline
namespace: company.data
tasks:
  - id: extract
    type: io.kestra.plugin.core.http.Request
    uri: https://api.example.com/data
  - id: transform
    type: io.kestra.plugin.scripts.python.Script
    script: |
      import json
      data = json.loads('{{ outputs.extract.body }}')
      # transform...
  - id: load
    type: io.kestra.plugin.jdbc.postgresql.Query
    sql: "INSERT INTO ..."
errors:
  - id: alert
    type: io.kestra.plugin.notifications.slack.SlackMessage
    message: "Pipeline failed!"
retries:
  - type: constant
    maxAttempt: 3
    interval: PT5S
triggers:
  - id: daily
    type: io.kestra.core.models.triggers.types.Schedule
    cron: "0 0 * * *"
```

**Key characteristics:**
- Two task categories: **Flowable** (control flow -- branching, looping, parallel) and **Runnable** (computational work on workers).
- Built-in expression language with `{{ }}` syntax for variable passing.
- Rich error handling: `errors` block, `finally` block, `allowFailure`, `retries` with backoff.
- Plugin architecture (500+ integrations).
- Not a durable execution engine -- more like a next-gen Airflow with YAML.

### 5.4 Dagu

**Source:** [github.com/dagu-org/dagu](https://github.com/dagu-org/dagu)

A **local-first**, single-binary workflow engine using declarative YAML.

```yaml
type: graph
steps:
  - id: step_1
    command: echo "Step 1"
  - id: step_2a
    command: echo "Runs in parallel"
    depends: [step_1]
  - id: step_2b
    command: echo "Also parallel"
    depends: [step_1]
  - id: step_3
    command: echo "Waits for both"
    depends: [step_2a, step_2b]
```

**Key characteristics:**
- Two modes: `chain` (sequential) and `graph` (DAG with explicit `depends`).
- Implicit parallelism from dependency graph.
- Sub-DAG composition via `call` directive.
- Variable passing: `${step_name.outputs.result}` syntax.
- 19+ built-in executors (HTTP, SQL, Redis, S3, containers, SSH, etc.).
- Zero external dependencies (file-based storage, no database needed).
- Not a durable execution engine -- process-level execution.

### 5.5 GraphAI

**Source:** [github.com/receptron/graphai](https://github.com/receptron/graphai)

An **asynchronous data flow execution engine** for agentic AI applications.

**Key characteristics:**
- Workflows described as declarative data flow graphs in YAML/JSON.
- Nodes are "agents" (LLM calls, API calls, database queries, etc.).
- Edges represent data dependencies.
- Engine handles concurrent async calls, data dependency management, map-reduce, error handling, retries.
- Supports loops, conditionals, and MapReduce.
- Designed specifically for multi-agent AI systems.
- TypeScript/Node.js runtime.

### 5.6 Restate

**Source:** [restate.dev](https://www.restate.dev/)

Not a declarative DSL, but notable for its **alternative approach to durable execution** that solves the versioning/immutability problem differently from Temporal.

**Key insight:** Instead of replaying entire workflow histories (Temporal's approach), Restate uses **immutable deployments** -- old versions are kept running only until in-flight invocations complete (usually minutes). This avoids the need for version branching (`workflow.GetVersion()`) in code.

**Relevance to declarative workflows:** Restate's model suggests that declarative workflows over durable execution could avoid the worst versioning pitfalls if the underlying engine uses immutable deployments rather than history replay.

---

## 6. Comparative Analysis

| Feature | Temporal DSL Sample | Conductor | Hatchet | Windmill | Kestra | Serverless WF | Dagu |
|---------|-------------------|-----------|---------|----------|--------|---------------|------|
| **DSL Format** | YAML | JSON | Code (Python/TS) | OpenFlow JSON | YAML | YAML/JSON | YAML |
| **Execution Engine** | Temporal | Conductor Server | Hatchet | Windmill | Kestra | Multiple | Single binary |
| **Durable Execution** | Yes (Temporal) | Yes (server-side) | Yes | No | No | Depends on runtime | No |
| **Parallelism** | Explicit `parallel` block | FORK_JOIN / DYNAMIC_FORK | Implicit from DAG | BranchAll / for-loop | Flowable tasks | Parallel state | Implicit from `depends` |
| **Variable Passing** | Shared string map | JSONPath expressions | Typed ctx.task_output() | JS expressions | `{{ }}` expressions | JQ expressions | `${step.outputs}` |
| **Error Handling** | Bubble up + cancel | Per-task retry + failure workflow | Per-task retry + timeout | Retry + error handler + early stop | errors/finally/retries blocks | Retry + error policies | Configurable |
| **Conditionals** | None | Switch operator | Code-level | BranchOne | Switch task | Switch state | Code-level |
| **Loops** | None | Do-While operator | Code-level | For-loop | ForEachItem | ForEach state | Code-level |
| **Maturity** | Sample only | Production (Netflix-scale) | Growing | Production | Production | Spec + implementations | Production |
| **Target Users** | Developers | Platform teams | Developers | Mixed (low-code + code) | Data/Platform teams | Spec authors | DevOps/SRE |

---

## 7. Patterns That Worked vs. Failed

### Patterns That Succeeded

**1. Declarative for structure, code for logic ("Hybrid" approach)**
The most successful tools (Hatchet, Windmill, Conductor) let users declare the workflow DAG/structure declaratively but keep individual step logic in real code. This avoids the "Turing tarpit" where a DSL tries to become a programming language.

**2. Implicit parallelism from dependency graphs**
Hatchet and Dagu both derive parallelism automatically from declared dependencies rather than requiring explicit parallel/fork constructs. This is simpler to author and less error-prone.

**3. JSONPath/expression-based variable passing**
Conductor's `${taskRef.output.key}` and Kestra's `{{ outputs.step.value }}` patterns are well-understood and scale to complex data flows without requiring users to manage state explicitly.

**4. Typed task outputs with schema validation**
Conductor's schema validation and Hatchet's typed outputs catch data flow errors early. Untyped string maps (as in the Temporal DSL sample) become unmanageable at scale.

**5. Built-in control flow operators**
Conductor's Switch, Do-While, Fork/Join operators provide enough expressiveness for most business workflows without requiring code-level control flow. These are well-tested patterns.

**6. Separation of orchestration and execution**
All successful tools separate the workflow definition (what to do) from the task execution (how to do it). This enables different teams to own different parts.

### Patterns That Failed or Struggled

**1. Trying to make YAML Turing-complete**
DSLs that add conditionals, loops, and complex expressions to YAML inevitably create a worse programming language. The "inner platform effect" -- building a language inside a language -- leads to poor debugging, no IDE support, and frustrated developers.

**2. String-only variable passing (Temporal DSL sample)**
The sample's `map[string]string` bindings model collapses when you need structured data, arrays, or nested objects. Every production DSL-over-Temporal implementation needs richer data types.

**3. Ignoring the versioning problem**
Declarative workflows over durable execution engines inherit all versioning complexity. When a YAML workflow definition changes while instances are in-flight, the system must handle:
- Non-determinism errors (Temporal replay fails if the DAG shape changes).
- Migration of in-flight executions to new definitions.
- Backward-compatible changes vs. breaking changes.

**Production horror stories:** CPU/memory spikes from failed replays, infinite retry loops consuming entire worker fleets, payload size limits silently exceeded.

**4. Polling-based task dispatch (Conductor)**
Conductor's worker-polling model adds latency compared to direct invocation. For latency-sensitive workflows, this is a significant drawback.

**5. Overly abstract DSLs that hide the execution model**
When a DSL hides too much of the underlying engine (e.g., Temporal's determinism constraints), users accidentally write non-deterministic workflows. The DSL must either enforce constraints or clearly communicate them.

**6. No escape hatch to code**
Pure declarative systems (YAML/JSON only) hit a wall when business logic gets complex. The most successful tools provide an escape hatch: Windmill lets you write code steps, Hatchet supports durable tasks alongside DAGs, Conductor has inline code evaluation.

### Key Lessons

1. **The right abstraction level is "declare the graph, code the nodes."** Declare dependencies, parallelism, and error policies in YAML/JSON. Keep step logic in real programming languages.

2. **Versioning is the hardest unsolved problem.** Declarative workflows make it *seem* easy to change definitions, but the underlying durable execution engine may not tolerate changes to in-flight workflows. Restate's immutable deployment model is the most promising approach to solving this.

3. **Expression languages are a slippery slope.** Start with simple variable references (`${step.output.key}`). JQ and JavaScript expression engines add power but also complexity, debugging difficulty, and security concerns.

4. **Visual editors and YAML are complementary, not competing.** Kestra and Windmill both offer visual editors that generate YAML/JSON. The YAML is the source of truth; the editor is a productivity tool. This dual-mode approach serves both developer and non-developer users.

5. **Retry and timeout configuration belongs in the declarative layer.** These are operational concerns that should be tunable without code changes. Every successful system puts them in the workflow definition.

6. **The CNCF Serverless Workflow spec is the closest thing to a standard** but adoption is limited. Zigflow's approach of mapping it to Temporal is promising but young. Building on this spec reduces vendor lock-in risk.

---

## Sources

- [Temporal Go SDK Samples - DSL](https://github.com/temporalio/samples-go/tree/main/dsl)
- [Zigflow - Temporal DSL](https://zigflow.dev/)
- [Orkes Conductor - Workflows](https://orkes.io/content/developer-guides/workflows)
- [Orkes Conductor - Wiring Parameters](https://orkes.io/content/developer-guides/passing-inputs-to-task-in-conductor)
- [Orkes Conductor - Dynamic Workflows with Operators](https://dev.to/orkes/control-the-flow-building-dynamic-workflows-with-orkes-operators-2fo9)
- [Hatchet - DAGs](https://docs.hatchet.run/home/dags)
- [Hatchet - Durable Workflows](https://docs.hatchet.run/v1/durable-workflows-overview)
- [Windmill - OpenFlow](https://www.windmill.dev/docs/openflow)
- [Windmill - Flow Editor](https://www.windmill.dev/docs/flows/flow_editor)
- [Kestra - Flows](https://kestra.io/docs/workflow-components/flow)
- [Kestra - Error Handling](https://kestra.io/docs/workflow-components/errors)
- [CNCF Serverless Workflow Specification](https://github.com/serverlessworkflow/specification)
- [Serverless Workflow Spec - Ionflow Docs](https://docs.kuberkai.com/concepts/serverless-workflow-spec/)
- [Dagu](https://github.com/dagu-org/dagu)
- [GraphAI](https://github.com/receptron/graphai)
- [Restate - Solving Durable Execution's Immutability Problem](https://www.restate.dev/blog/solving-durable-executions-immutability-problem)
- [Common Pitfalls with Durable Execution Frameworks](https://medium.com/@cgillum/common-pitfalls-with-durable-execution-frameworks-like-durable-functions-or-temporal-eaf635d4a8bb)
- [Workflow Should be Code, but Durable Execution is NOT the ONLY Way](https://medium.com/@qlong/workflow-should-be-code-but-durable-execution-is-not-the-only-way-519f7682360c)
- [DSL-Based Workflow Orchestration - Architecture](https://medium.com/@nareshvenkat14/dsl-based-workflow-orchestration-part-1-introduction-architecture-9d0112f77e00)
- [Workflow Orchestration Platforms Comparison 2025](https://procycons.com/en/blogs/workflow-orchestration-platforms-comparison-2025/)
