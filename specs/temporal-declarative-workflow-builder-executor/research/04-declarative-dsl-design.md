# Declarative Workflow DSL Design Patterns

Research into existing YAML-based workflow definition languages for informing the design of a Temporal-backed declarative executor in kagent.

---

## 1. Argo Workflows

Argo Workflows is the most mature Kubernetes-native YAML workflow engine. Its CRD-based design is directly relevant to kagent.

### YAML Schema Structure

A Workflow spec contains an `entrypoint` and a list of `templates`. Each template can be one of: container, script, DAG, steps, resource, or suspend.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: example-
spec:
  entrypoint: main
  arguments:
    parameters:
    - name: message
      value: "hello"
  templates:
  - name: main
    dag:
      tasks:
      - name: A
        template: echo
        arguments:
          parameters: [{name: message, value: "{{inputs.parameters.message}}"}]
      - name: B
        dependencies: [A]
        template: echo
        arguments:
          parameters: [{name: message, value: "{{tasks.A.outputs.parameters.result}}"}]
```

### DAG Templates

Dependencies are explicit arrays: `dependencies: [A, B]`. Tasks with no dependencies run immediately. Multiple roots are supported. Built-in fail-fast behavior (configurable with `failFast: false`) stops scheduling new tasks when any node fails.

### Steps Templates

Steps use nested lists: outer lists run sequentially, inner lists run in parallel.

```yaml
templates:
- name: main
  steps:
  - - name: step1      # sequential group 1
      template: taskA
  - - name: step2a     # sequential group 2 (parallel)
      template: taskB
    - name: step2b
      template: taskC
```

### Inputs/Outputs and Parameter Passing

- **Interpolation syntax:** `{{inputs.parameters.name}}`, `{{steps.STEP.outputs.parameters.NAME}}`, `{{tasks.TASK.outputs.parameters.NAME}}`
- **Artifacts:** File-based data passing. Outputs declare a `path` on disk; inputs unpack to a `path`. Referenced via `{{steps.NAME.outputs.artifacts.ART_NAME}}`.
- **Parameter results:** Steps can export parameters from file contents or expressions.

```yaml
outputs:
  artifacts:
  - name: hello-art
    path: /tmp/hello_world.txt
  parameters:
  - name: result
    valueFrom:
      path: /tmp/result.txt
```

### Retry/Timeout Policies

```yaml
retryStrategy:
  limit: 10
  retryPolicy: "Always"        # Always | OnFailure | OnError | OnTransientError
  backoff:
    duration: "1"               # initial wait (string, seconds)
    factor: 2                   # exponential multiplier
    maxDuration: "1m"           # ceiling on backoff
  affinity:
    nodeAntiAffinity: {}        # retry on different node
```

Timeouts are set at the workflow or template level:
```yaml
activeDeadlineSeconds: 300      # overall timeout
```

### What Works Well

- Explicit dependency declaration in DAGs is intuitive and readable.
- Template reuse via `WorkflowTemplate` CRD encourages composability.
- Artifact passing is well-designed for container-based workloads.
- Retry policy is granular (per-template, per-step).

### What Is Overly Complex

- The double-nested list syntax for steps (parallel-within-sequential) is confusing to newcomers.
- Variable interpolation uses `{{}}` which collides with Go templates, Helm, and other K8s tooling.
- The distinction between parameters and artifacts adds cognitive overhead when simple string data would suffice.
- No native expression language for conditionals -- relies on `when` clauses with limited operators.

---

## 2. Tekton Pipelines

Tekton is a Kubernetes-native CI/CD framework. Its CRD model separates Tasks (units of work) from Pipelines (orchestration).

### Task CRD

A Task contains ordered `steps`, each a container spec. Steps execute sequentially within a single Pod.

```yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: build
spec:
  params:
  - name: image-url
    type: string
  results:
  - name: image-digest
    type: string
  workspaces:
  - name: source
  steps:
  - name: build
    image: kaniko
    script: |
      /kaniko/executor --destination=$(params.image-url)
      echo -n "sha256:abc" | tee $(results.image-digest.path)
```

### Pipeline CRD

Pipelines compose Tasks with explicit ordering and data flow.

```yaml
apiVersion: tekton.dev/v1
kind: Pipeline
metadata:
  name: ci-pipeline
spec:
  params:
  - name: repo-url
    type: string
  workspaces:
  - name: shared-workspace
  tasks:
  - name: clone
    taskRef:
      name: git-clone
    params:
    - name: url
      value: $(params.repo-url)
    workspaces:
    - name: output
      workspace: shared-workspace
  - name: build
    taskRef:
      name: build
    runAfter: [clone]
    params:
    - name: commit
      value: $(tasks.clone.results.commit-sha)
    workspaces:
    - name: source
      workspace: shared-workspace
```

### Param/Result Flow Between Tasks

- **Interpolation syntax:** `$(params.name)`, `$(tasks.TASK.results.RESULT)`
- **Results** are written to files at `$(results.NAME.path)` -- limited to 4096 bytes (Kubernetes termination message constraint).
- **Implicit ordering:** Referencing a task's result automatically creates a dependency, in addition to explicit `runAfter`.

### Workspace Model

Workspaces are Tekton's primary mechanism for sharing large data between tasks. They map to PVCs, ConfigMaps, Secrets, or emptyDirs. This is more flexible than Argo's artifact model for file-heavy workflows.

### Key Design Insights

- **Separation of Task and Pipeline** is a strong pattern: tasks are reusable, pipelines compose them. This maps well to Temporal activities vs workflows.
- **Typed params** (string, array, object) with JSON Schema-like properties provide good validation.
- **Result size limit** (4KB) forces clean data contracts between tasks -- large data goes through workspaces.
- **`runAfter` + implicit result dependencies** is an elegant dual-mode ordering system.

### Weaknesses

- Workspace mapping between pipeline and task levels is verbose.
- No native expression language -- everything is string interpolation.
- No built-in retry at the task level within a pipeline (relies on PipelineRun-level configuration).

---

## 3. GitHub Actions

The most widely adopted workflow YAML format, optimized for developer experience.

### Workflow Structure

```yaml
name: CI
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        node: [18, 20, 22]
    steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-node@v4
      with:
        node-version: ${{ matrix.node }}
    - run: npm test

  deploy:
    needs: [test]
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
    - run: echo "deploying..."

  notify:
    needs: [deploy]
    if: always()
    runs-on: ubuntu-latest
    outputs:
      status: ${{ steps.check.outputs.result }}
    steps:
    - id: check
      run: echo "result=success" >> $GITHUB_OUTPUT
```

### Job Dependencies and Outputs

- **`needs`** declares job dependencies (string or array).
- **Outputs** flow from steps to jobs to downstream jobs:
  - Step sets output: `echo "key=value" >> $GITHUB_OUTPUT`
  - Job declares output: `outputs: { key: ${{ steps.STEP.outputs.key }} }`
  - Downstream job reads: `${{ needs.JOB.outputs.key }}`

### Expression Syntax: `${{ }}`

- Context objects: `github`, `env`, `secrets`, `inputs`, `needs`, `steps`, `matrix`, `strategy`, `runner`, `job`
- Functions: `contains()`, `startsWith()`, `endsWith()`, `format()`, `toJSON()`, `fromJSON()`, `hashFiles()`
- Operators: `==`, `!=`, `&&`, `||`, `!`
- Type coercion rules (null -> 0/false/"")

### Matrix Strategies

```yaml
strategy:
  matrix:
    os: [ubuntu-latest, windows-latest]
    version: [1.0, 2.0]
    exclude:
    - os: windows-latest
      version: 1.0
    include:
    - os: macos-latest
      version: 2.0
  fail-fast: true
  max-parallel: 4
```

### Error Handling

- `continue-on-error: true` -- step/job continues on failure
- `timeout-minutes: N` -- per-job timeout
- `if: failure()` / `if: always()` / `if: cancelled()` -- conditional execution based on status

### Design Strengths

- **Expression syntax `${{ }}`** is distinctive enough to avoid collisions with shell variables, JSON, YAML native syntax.
- **Matrix strategy** is a powerful pattern for parameterized parallel execution.
- **`needs` + `if`** creates a clean dependency/conditional model.
- **Huge ecosystem adoption** validates the YAML schema choices.

### Design Weaknesses

- No native DAG support -- only linear `needs` chains.
- Output passing between jobs is verbose (3-level indirection: step -> job output -> needs reference).
- No retry mechanism at the step level (only third-party actions).
- Expression syntax does not support nested interpolation.

---

## 4. Prefect / Dagster

### Dagster: Asset-Centric Declarative Approach

Dagster supports YAML-based DSLs for defining asset pipelines. The key insight is treating **data assets** (not tasks) as the primary abstraction.

```yaml
group_name: pipeline
assets:
  - asset_key: "raw/events"
    sql: "SELECT * FROM source_events"
  - asset_key: "cleaned/events"
    description: "Deduplicated events"
    deps:
      - "raw/events"
    sql: "INSERT INTO cleaned_events SELECT DISTINCT * FROM raw_events"
```

**Design patterns:**
- **`deps` for dependencies** -- simple, asset-key-based references.
- **Escape hatch pattern** -- critical best practice: always allow users to insert custom code/tasks not natively supported by the DSL. Prevents the DSL from becoming a limiting factor.
- **Target audience** -- DSLs empower non-engineers (analysts, PMs) to define pipelines.

### Prefect: Code-First, Declarative Config

Prefect is primarily imperative (Python-first), not YAML-declarative. Configuration is declarative (deployment specs, schedules), but workflow logic is code.

**Relevant patterns:**
- **Rich state management** -- tasks have states (Pending, Running, Completed, Failed, Cached) with automatic transitions.
- **Retry with context** -- retries carry state from the failed attempt.
- **Result persistence** -- task outputs are serialized and stored, enabling caching and data passing.

### Key Takeaway

Modern data orchestrators are moving away from YAML-for-logic toward YAML-for-configuration with code-for-logic. Dagster's DSL approach works because it limits scope to asset definitions (SQL queries) rather than trying to express arbitrary control flow in YAML.

---

## 5. Variable Interpolation Patterns

### Pattern Comparison

| Pattern | Used By | Pros | Cons |
|---------|---------|------|------|
| `{{variable}}` | Argo, Helm, Go templates | Familiar to K8s ecosystem | Collides with Helm/Go templates; ambiguous escaping |
| `${{ expression }}` | GitHub Actions | Distinct from shell vars; supports expressions | Verbose for simple refs; no nesting |
| `$(params.name)` | Tekton | Simple; shell-like; clear namespace | Collides with shell command substitution `$(cmd)` |
| `${expression}` | Serverless Workflow, Zigflow | Concise; supports JQ/JSONPath | Collides with shell variable expansion |
| `${ expression }` | Zigflow (spaced variant) | Less collision risk than `${}` | Still ambiguous in shell contexts |
| `{% expr %}` / `{{ var }}` | Jinja2 (Ansible, dbt) | Powerful templating; filters; loops | Turing-complete creep; hard to debug |

### Recommendations for kagent

1. **Avoid `{{}}`** -- too many collisions in the Kubernetes ecosystem (Helm, Go templates).
2. **`${{ }}` (GitHub Actions style)** is the strongest candidate: visually distinct, supports expressions, widely understood.
3. **Keep the expression language minimal** -- variable references, property access, basic comparisons. Avoid Turing-completeness.
4. **Namespace variables clearly**: `${{ inputs.param_name }}`, `${{ steps.step_name.outputs.result }}`, `${{ workflow.name }}`.
5. **Support literal escaping**: `$${{ }}` to produce literal `${{ }}` in output.

---

## 6. Output Mapping Patterns

### Comparison of Approaches

| Mechanism | Tools | Best For | Limitations |
|-----------|-------|----------|-------------|
| **Named parameters** | Argo, Tekton, GH Actions | Small string data (< 4KB) | Size limits; no binary data |
| **Artifacts (files)** | Argo | Large/binary data | Requires storage backend (S3, GCS) |
| **Shared volumes/workspaces** | Tekton | Large data within a pipeline | K8s PVC dependency; not portable |
| **Context objects** | GH Actions, Zigflow | Step-to-step within a job | Scoped to execution context |
| **Environment variables** | GH Actions | Simple key-value | No structured data; size limits |
| **Export/set directives** | Zigflow, Serverless Workflow | Accumulating workflow state | Can create implicit dependencies |

### Argo: Dual-Track (Parameters + Artifacts)

```yaml
# Producer
outputs:
  parameters:
  - name: result
    valueFrom:
      path: /tmp/result.txt
  artifacts:
  - name: report
    path: /tmp/report.pdf

# Consumer (in DAG)
arguments:
  parameters:
  - name: input
    value: "{{tasks.producer.outputs.parameters.result}}"
  artifacts:
  - name: report
    from: "{{tasks.producer.outputs.artifacts.report}}"
```

### Tekton: Results + Workspaces

```yaml
# Producer task writes result
script: |
  echo -n "sha256:abc" | tee $(results.digest.path)

# Consumer task reads result
params:
- name: digest
  value: $(tasks.build.results.digest)
```

Large data passes through shared workspaces (PVCs).

### GitHub Actions: Step Outputs + Job Outputs + Artifacts

Three levels of data passing:
1. **Within a job:** `$GITHUB_OUTPUT` file for step outputs, accessed via `${{ steps.ID.outputs.KEY }}`
2. **Between jobs:** Job-level `outputs` map, accessed via `${{ needs.JOB.outputs.KEY }}`
3. **Between workflows:** `actions/upload-artifact` / `actions/download-artifact`

### Zigflow/Serverless Workflow: Context Accumulation

```yaml
- processData:
    call: http
    with:
      endpoint: https://api.example.com
    export:
      as: '${ $context + { processedResult: .body } }'
```

Data accumulates in `$context` and `$data` objects, accessible by subsequent steps.

### Recommendation for kagent

For a Temporal-backed system:
- **Primary mechanism:** Named outputs (string/JSON) mapped between activities. This aligns with Temporal's activity input/output model.
- **Large data:** Reference-based passing (URLs, object store keys) rather than inline data. Temporal has payload size limits.
- **Context object:** A workflow-level context that accumulates step outputs, similar to Zigflow's `$context`.

---

## 7. Error/Retry Policy Schemas

### Argo Workflows

```yaml
retryStrategy:
  limit: 10                          # max retries
  retryPolicy: "Always"              # Always | OnFailure | OnError | OnTransientError
  backoff:
    duration: "1"                    # initial backoff (seconds)
    factor: 2                        # exponential multiplier
    maxDuration: "1m"                # backoff ceiling
  affinity:
    nodeAntiAffinity: {}             # retry on different node
```

Applied per-template. No workflow-level default retry.

### Tekton

No built-in retry at the task level within a pipeline. Retries are configured on PipelineRun:

```yaml
apiVersion: tekton.dev/v1
kind: PipelineRun
spec:
  pipelineRef:
    name: my-pipeline
  taskRunTemplate:
    retries: 3
```

Steps within a task support `onError: continue | stopAndFail` and `timeout: 5s`.

### GitHub Actions

No native retry. Error handling is status-based:

```yaml
steps:
- run: flaky-command
  continue-on-error: true
  timeout-minutes: 10
- if: failure()
  run: echo "previous step failed"
```

### Serverless Workflow Specification (v1.0)

```yaml
do:
- tryTask:
    try:
      call: http
      with:
        method: get
        endpoint: https://unstable-api.example.com
    catch:
      errors:
        with:
          type: https://serverlessworkflow.io/spec/1.0.0/errors/timeout
          status: 408
      retry:
        delay:
          seconds: 3
        backoff:
          exponential: {}
        limit:
          attempt:
            count: 5
          duration:
            minutes: 10
      do:
        - fallbackTask:
            call: http
            with:
              endpoint: https://fallback-api.example.com
```

Features: error type matching, retry with backoff strategies (constant, exponential, linear), attempt and duration limits, fallback task chains.

### Zigflow (Temporal DSL)

```yaml
metadata:
  activityOptions:
    retryPolicy:
      maximumAttempts: 5
    heartbeatTimeout:
      seconds: 10
```

Maps directly to Temporal's native retry policy.

### Comparison Summary

| Feature | Argo | Tekton | GH Actions | Serverless WF | Zigflow |
|---------|------|--------|------------|----------------|---------|
| Per-step retry | Yes | No | No | Yes | Yes |
| Backoff strategies | Exponential | N/A | N/A | Exp/Linear/Const | Via Temporal |
| Timeout | Yes | Per-step | Per-job | Yes | Yes |
| Error type matching | No | No | No | Yes (URI-based) | No |
| Fallback/compensation | No | No | No | Yes (catch.do) | No |
| Continue on error | No | Per-step | Per-step/job | No | No |

### Recommendation for kagent

Since the backend is Temporal, the retry policy schema should map closely to Temporal's native `RetryPolicy`:

```yaml
retry:
  maxAttempts: 5                     # maps to MaximumAttempts
  initialInterval: 1s               # maps to InitialInterval
  backoffCoefficient: 2.0           # maps to BackoffCoefficient
  maxInterval: 60s                  # maps to MaximumInterval
  nonRetryableErrors:               # maps to NonRetryableErrorTypes
  - "INVALID_INPUT"
  - "PERMISSION_DENIED"
timeout:
  scheduleToClose: 5m              # overall timeout
  startToClose: 2m                 # per-attempt timeout
  heartbeat: 30s                   # heartbeat timeout
```

Additionally, consider Serverless Workflow's `try/catch/do` pattern for fallback chains, as it adds value beyond basic retries.

---

## 8. Lessons Learned

### Common Mistakes in Workflow DSL Design

#### 1. Turing-Completeness Creep

The most common and dangerous anti-pattern. DSLs start simple, then accumulate:
- Conditional branching (`if/else`)
- Loops (`for/while`)
- Variable assignment and mutation
- String manipulation functions
- Custom expression languages

Before long, the DSL becomes a poorly-designed programming language without proper tooling (debuggers, type checkers, IDEs). As Martin Fowler notes, DSLs should be "limited both in scope and capability."

**Mitigation:** Define a strict boundary upfront. The DSL declares *what* to run and *when*; a real programming language defines *how*. Provide an "escape hatch" to code (Dagster's pattern).

#### 2. Over-Abstraction

Building a "flexible" configuration system that can handle any future requirement, when a simpler solution would work. Indeed's production experience showed that DSL/graph/JSON-based workflow engines fail when forced to support complex branching and loops.

**Mitigation:** Start with the simplest schema that solves current needs. Add features only when real usage demands them.

#### 3. Debugging Difficulty

YAML workflows are notoriously hard to debug:
- No stack traces in YAML.
- Variable interpolation errors surface at runtime, not parse time.
- Template expansion makes it hard to see the "final" workflow.
- No breakpoints or step-through debugging.

**Mitigation:**
- Provide a `--dry-run` mode that expands all variables and shows the resolved workflow.
- Validate schemas at parse time with clear error messages.
- Include a workflow visualization tool.
- Log resolved variable values at each step.

#### 4. Schema Evolution Challenges

Workflow DSLs must evolve without breaking existing definitions:
- Adding required fields breaks existing workflows.
- Changing field semantics silently changes behavior.
- Versioning workflow definitions is often an afterthought.

**Mitigation:**
- Include a `version` or `dsl` field from day one (as Serverless Workflow and Zigflow do).
- All new fields must be optional with sensible defaults.
- Maintain schema validation per version.
- Provide migration tooling for version upgrades.

#### 5. Impedance Mismatch with Execution Engine

The DSL's abstractions may not map cleanly to the execution engine's model. For Temporal specifically:
- Long-running workflows require versioning because replay fails if workflow code changes.
- Determinism constraints (no random, no time, no I/O in workflow code) must be respected.
- Signal/query/update semantics need explicit DSL support.

**Mitigation:** Design the DSL as a thin layer over Temporal's concepts, not an abstraction that hides them. Users should understand they are building Temporal workflows.

#### 6. Variable Scoping Ambiguity

When multiple interpolation syntaxes coexist (shell variables, YAML anchors, DSL expressions), users cannot tell which system evaluates what and when.

**Mitigation:**
- Use a single, distinctive interpolation syntax.
- Document evaluation order clearly.
- Reject ambiguous expressions at parse time.

### Positive Patterns to Adopt

1. **Explicit > Implicit:** Argo's explicit `dependencies` array is clearer than Tekton's implicit result-based ordering.
2. **Separation of concerns:** Tekton's Task/Pipeline split maps naturally to Temporal's Activity/Workflow split.
3. **Type-safe parameters:** Tekton's typed params (string, array, object) catch errors early.
4. **Context accumulation:** Zigflow's `$context` pattern simplifies data flow without requiring explicit output wiring for every step.
5. **Version field:** Serverless Workflow's `dsl: 1.0.0` enables graceful schema evolution.
6. **Escape hatch:** Dagster's pattern of allowing custom code alongside DSL definitions prevents the DSL from becoming a bottleneck.

---

## Summary: Design Principles for kagent Workflow DSL

Based on this research, the following principles should guide the kagent declarative workflow DSL:

1. **Thin layer over Temporal** -- The DSL should expose Temporal concepts (workflows, activities, signals, queries, retries) rather than inventing new abstractions.
2. **`${{ }}` interpolation** -- Adopt GitHub Actions-style expressions. Visually distinct, widely understood, supports basic expressions.
3. **Explicit dependencies** -- Use Argo-style `dependencies: [taskA, taskB]` arrays for DAG ordering.
4. **Task/Workflow separation** -- Follow Tekton's pattern: define reusable tasks (activities) separately from workflow orchestration.
5. **Typed inputs/outputs** -- Support string, object, and array types with JSON Schema validation.
6. **Temporal-native retry/timeout** -- Map directly to Temporal's RetryPolicy and timeout fields rather than inventing a new abstraction.
7. **Schema versioning from day one** -- Include `apiVersion` and `kind` fields (Kubernetes convention) or `dsl` version field.
8. **Dry-run and validation** -- Provide tooling to resolve variables and validate workflows before execution.
9. **Escape hatch to code** -- Allow steps that reference Go/Python functions for logic that cannot be expressed declaratively.
10. **Resist Turing-completeness** -- No loops, no variable mutation, no arbitrary conditionals in the DSL. Use Temporal's native workflow logic for complex control flow.

---

## Sources

- [Argo Workflows DAG Documentation](https://argo-workflows.readthedocs.io/en/latest/walk-through/dag/)
- [Argo Workflows Artifacts](https://argo-workflows.readthedocs.io/en/latest/walk-through/artifacts/)
- [Argo Workflows Retries](https://argo-workflows.readthedocs.io/en/latest/retries/)
- [Argo Workflows Retry Backoff Example](https://github.com/argoproj/argo-workflows/blob/main/examples/retry-backoff.yaml)
- [Tekton Tasks Documentation](https://tekton.dev/docs/pipelines/tasks/)
- [Tekton Pipelines Documentation](https://tekton.dev/docs/pipelines/pipelines/)
- [GitHub Actions Workflow Syntax](https://docs.github.com/actions/using-workflows/workflow-syntax-for-github-actions)
- [GitHub Actions Expressions](https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/evaluate-expressions-in-workflows-and-actions)
- [Dagster: Scale and Standardize Pipelines with DSL](https://dagster.io/blog/scale-and-standardize-data-pipelines-with-dsl)
- [Dagster: SimpliSafe YAML DSL Case Study](https://dagster.io/blog/simplisafe-case-study)
- [Serverless Workflow Specification](https://github.com/serverlessworkflow/specification)
- [Serverless Workflow DSL Reference](https://github.com/serverlessworkflow/specification/blob/main/dsl-reference.md)
- [Zigflow: A Temporal DSL](https://zigflow.dev/)
- [Martin Fowler: DSL Boundary](https://martinfowler.com/bliki/DslBoundary.html)
- [Martin Fowler: DSL Catalog](https://martinfowler.com/dslCatalog/)
- [Using Temporal and YAML as DSL for Orchestration](https://medium.com/@surajsub_68985/using-temporal-and-yaml-as-dsl-for-orchestration-3fa38405f65d)
- [Temporal DSL Code Exchange](https://temporal.io/code-exchange/temporal-dsl)
- [Workflow Should be Code, but Durable Execution is NOT the ONLY Way](https://medium.com/@qlong/workflow-should-be-code-but-durable-execution-is-not-the-only-way-519f7682360c)
- [Pipekit: How to Set Up Retries for Argo Workflows](https://pipekit.io/blog/set-up-retries-argo-workflows)
