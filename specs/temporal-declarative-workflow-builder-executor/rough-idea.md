# Rough Idea: Temporal Declarative Workflow Builder and Executor

## Problem

Kagent already uses Kubernetes-native declarative APIs for agents and infrastructure, but workflow orchestration still tends to require imperative code. Temporal is a strong runtime for durable orchestration, retries, and long-running execution, yet its default development model is code-first.

This creates a gap:
- Platform teams want GitOps-friendly, declarative workflow definitions.
- Application teams want reliable execution semantics (retries, timeouts, compensation, observability).
- Current solutions often force users to pick one side (declarative or programmable), not both.

## Proposed Idea

Build a **declarative workflow builder and executor** for Temporal in the kagent ecosystem.

Users define workflows as YAML (and later CRDs), and kagent compiles/translates those definitions into Temporal workflow execution plans. The runtime then executes those plans on Temporal while preserving declarative intent and Kubernetes operational patterns.

In short: **declarative authoring + durable Temporal execution**.

## Goals

- Provide a simple declarative DSL for workflow definitions.
- Support both sequential and parallel execution patterns.
- Support retries, timeouts, and failure policies per step.
- Enable reusable step templates and parameterized workflows.
- Allow per-step runtime image selection for containerized execution.
- Allow steps to call kagent Agents and store returned outputs in context.
- Keep workflow definitions GitOps-friendly and reviewable.
- Expose workflow run status and history through kagent APIs/UI.
- use CEL expression interpolations

## Non-Goals (initial phase)

- Full visual designer on day one.
- Arbitrary Turing-complete logic inside YAML.
- Replacing Temporal SDKs for advanced custom workflow code.
- Building a new workflow engine (Temporal remains the executor).

## High-Level Architecture

1. **Workflow Definition API**
   - Start with YAML schema in `specs/` and optional file-based loading.
   - Evolve into CRDs (for example, `WorkflowTemplate` and `WorkflowRun`).

2. **Compiler/Translator**
   - Parse and validate declarative spec.
   - Build a DAG-like execution graph.
   - Translate nodes and edges into Temporal workflow + activity calls.

3. **Execution Runtime**
   - Submit translated workflows to Temporal.
   - Track run IDs, state transitions, retries, and final outcomes.
   - Persist metadata in kagent data model for API/UI retrieval.

4. **Observability Layer**
   - Surface status (`Pending`, `Running`, `Succeeded`, `Failed`, `Cancelled`).
   - Link Temporal execution details into kagent logs/events.
   - Emit metrics for step duration, retries, and failure reasons.

## Declarative Model (v1 shape)

Core concepts:
- `workflow`: top-level metadata and inputs.
- `steps`: units of work (maps to Temporal activities or child workflows).
- `step.type`: execution mode (`action` or `agent` in v1).
- `image`: optional container image used to run a step handler.
- `prompt`: templated instruction payload for `agent` steps.
- `dependsOn`: explicit ordering edges for DAG execution.
- `parallel`: sibling steps with no dependency edges.
- `policies`: retries, backoff, timeout, and failure behavior.
- `outputs`: named values exposed from steps/workflow.
- `context`: shared workflow-scoped key/value store populated by step outputs.

Example rough YAML:

```yaml
apiVersion: kagent.dev/v1alpha1
kind: WorkflowTemplate
metadata:
  name: build-and-test
spec:
  inputs:
    repoUrl: string
    commitSha: string
  steps:
    - name: checkout
      type: action
      action: git.clone
      image: ghcr.io/kagent-dev/git-tools:latest
      with:
        repoUrl: ${inputs.repoUrl}
        commitSha: ${inputs.commitSha}
      output:
        as: checkoutResult

    - name: unit-tests
      type: action
      action: ci.runTests
      image: ghcr.io/kagent-dev/ci-runner:latest
      dependsOn: [checkout]
      policy:
        retry:
          maxAttempts: 3
          backoff: exponential
        timeout: 15m

    - name: lint
      type: action
      action: ci.runLint
      image: ghcr.io/kagent-dev/ci-runner:latest
      dependsOn: [checkout]

    - name: summarize-results
      type: agent
      agentRef: test-results-analyst
      dependsOn: [unit-tests, lint]
      prompt: |
        Review the test and lint results, then return:
        - summary: short quality summary
        - qualityGate: one of PASS or FAIL
      with:
        testReport: ${context.unit-tests.report}
        lintReport: ${context.lint.report}
      output:
        keys:
          summary: analysisSummary
          qualityGate: qualityGateStatus

    - name: build
      type: action
      action: ci.buildImage
      image: ghcr.io/kagent-dev/buildkit-runner:latest
      dependsOn: [unit-tests, lint]
      with:
        tag: ${inputs.commitSha}
        releaseAllowed: ${context.qualityGateStatus}
```

This enables both:
- Sequential flow (`checkout -> build`).
- Parallel branches (`unit-tests` and `lint` after `checkout`).

## Execution Semantics

- Deterministic orchestration logic is maintained in Temporal workflow code generated/derived from the spec.
- Step actions map to:
  - Activity invocations for external calls.
  - Child workflows for reusable complex sequences.
- `image` determines the runtime environment for step execution when the step is containerized.
- `agent` steps call a referenced kagent Agent (`agentRef`) and can map selected return fields into workflow context.
- `prompt` is rendered with workflow variables (for example `${inputs.*}` and `${context.*}`) before agent invocation.
- Step output mapping:
  - `output.as` stores full step result at `context.<step-name>` or a custom alias.
  - `output.keys` stores selected fields as top-level context keys for easy downstream access.
- Retries/timeouts are applied from declarative policy fields.
- Failure handling modes (initial proposal):
  - `fail-fast`: stop workflow on first critical failure.
  - `continue-on-error`: continue non-critical branches.
  - `compensate`: invoke rollback/cleanup steps when defined.

## Validation and Safety

Before submission:
- Validate schema and required fields.
- Detect dependency cycles in the DAG.
- Validate references (`dependsOn`, variable interpolation paths).
- Validate agent references (`agentRef`) and output key mappings.
- Validate `agent` step prompt fields (non-empty after interpolation).
- Enforce guardrails (max parallelism, timeout bounds, retry limits).

Runtime safety:
- Idempotency guidance per action type.
- Clear cancellation semantics (propagate cancel to running branches).
- Secure secret handling via Kubernetes secrets references, not inline values.

## Integration Points with Kagent

- **CRDs**: future `WorkflowTemplate` and `WorkflowRun`.
- **Controller**: reconcile specs to Temporal submissions.
- **API/UI**: list templates, start runs, inspect run graph + step status.
- **Agent ecosystem**: allow steps to call kagent tools/agents as actions.

## Initial Milestones

1. Define v1 schema and validation rules.
2. Build translator for sequential + parallel DAG patterns.
3. Implement Temporal executor with retries and timeouts.
4. Add run status API and minimal UI status view.
5. Add examples (build pipeline, data processing pipeline).

## Open Questions

- Should we represent loops/conditionals in v1, or defer to v2?
- How much of Temporal advanced configuration should be exposed directly?
- Should reusable actions be modeled as named templates or tool references?
- What is the right split between CRD-driven and file-driven workflows initially?
- How do we version workflow definitions and support safe upgrades for active runs?

---

## Review Comments (Temporal Best Practices Audit)

### What Aligns Well

1. **Workflow as pure orchestration, steps as activities.** The spec correctly separates orchestration (the DAG) from business logic (step actions). Temporal's core pattern is: workflows orchestrate deterministically, activities execute side-effecting work. The statement on line 144 ("Deterministic orchestration logic is maintained in Temporal workflow code generated/derived from the spec") is exactly right.

2. **DAG-based execution model.** Mapping `dependsOn` edges to a DAG that compiles into activity/child-workflow scheduling is a sound approach. Temporal natively supports parallel activity execution and awaiting multiple results.

3. **Child workflows for reusable sequences.** Temporal recommends child workflows to partition large workloads, keep event histories bounded, and enable cross-team reuse.

4. **Retries with exponential backoff.** The spec includes retry policies with `maxAttempts` and `backoff: exponential`, which maps to Temporal's default retry behavior.

5. **Idempotency guidance.** Calling out idempotency per action type aligns with Temporal's strong recommendation that all activities be idempotent.

6. **DAG cycle detection.** Essential since Temporal workflows must terminate.

### Gaps and Issues

#### Critical

**C1: Incomplete Retry Policy Model**

The YAML only exposes `maxAttempts` and `backoff`. Temporal's retry policy has more fields that users will need:

| Temporal Field | Spec Coverage | Impact |
|---|---|---|
| `MaximumAttempts` | `maxAttempts` — covered | OK |
| `InitialInterval` | Missing | Users can't control first retry delay |
| `MaximumInterval` | Missing | Unbounded backoff is dangerous |
| `BackoffCoefficient` | `backoff: exponential` (too coarse) | Users can't tune the backoff curve |
| `NonRetryableErrorTypes` | Missing | No way to short-circuit on permanent failures |

Recommended shape:

```yaml
policy:
  retry:
    maxAttempts: 3
    initialInterval: 1s
    maximumInterval: 60s
    backoffCoefficient: 2.0
    nonRetryableErrors: ["INVALID_INPUT", "AUTH_FAILURE"]
```

**C2: Insufficient Timeout Model**

The spec has a single `timeout` field, but Temporal defines four distinct activity timeouts:

- **StartToCloseTimeout** — max time for a single attempt. Temporal docs: "recommended to ALWAYS set this."
- **ScheduleToCloseTimeout** — max total time including all retries.
- **ScheduleToStartTimeout** — max time waiting in a task queue (rarely needed).
- **HeartbeatTimeout** — max time between heartbeats for long-running activities.

A single `timeout: 15m` is ambiguous. A container build and an agent LLM call have very different timeout profiles.

Recommended shape:

```yaml
policy:
  timeout:
    startToClose: 15m
    scheduleToClose: 45m
    heartbeat: 30s
```

**C3: No Heartbeat Support**

For steps like `ci.runTests`, `ci.buildImage`, or agent invocations that can run for minutes, heartbeating is essential. Without it, a stalled activity won't be detected until the full timeout expires. Temporal best practice: always heartbeat long-running activities and set `HeartbeatTimeout` for faster failure detection.

Action: add heartbeat configuration to the step/policy model and ensure the executor runtime sends heartbeats from within containerized step execution.

#### Important

**I1: No Task Queue Configuration**

Temporal routes activities to workers via task queues. The `image` field implies different execution environments, but there is no way to control which task queue a step runs on. This matters for:
- Routing GPU-intensive agent steps to GPU workers.
- Isolating CI steps from agent steps.
- Scaling worker pools independently.

Action: add an optional `taskQueue` field per step or at the workflow level.

**I2: No Workflow ID Strategy**

Temporal uses workflow IDs for deduplication, idempotency, and correlation. The spec doesn't define how `WorkflowRun` names map to Temporal workflow IDs or what the ID reuse policy should be (`AllowDuplicate`, `RejectDuplicate`, `AllowDuplicateFailedOnly`, `TerminateIfRunning`).

Action: define a workflow ID generation strategy (e.g., `{template-name}-{run-name}`) and expose Temporal's `WorkflowIdReusePolicy`.

**I3: No Event History Size Awareness**

Temporal workflows have a ~50,000 event hard limit. A complex DAG with many parallel branches, retries, and agent calls could approach this. The spec doesn't mention:
- The `Continue-As-New` pattern for long or repetitive workflows.
- Event history monitoring.
- Maximum step count guardrails.

Action: add a note about Continue-As-New for workflows with many steps; set a max step count guardrail (e.g., 100 steps per template). For iterative/loop patterns (mentioned in open questions), Continue-As-New is mandatory.

**I4: Versioning for In-Flight Runs Must Be Addressed Before v1**

Currently listed as an open question. When a `WorkflowTemplate` is updated while runs are in-flight, the generated Temporal workflow code changes, causing **non-determinism errors** during replay. Temporal offers two approaches:
- **Worker Versioning** (recommended by Temporal): tag workers with build IDs, roll out versioned deployments.
- **Patching API**: branch code with version markers.

Action: the compiler/translator must generate versioned workflow code. When a template changes, new runs use the new version while in-flight runs continue on their original version. This is a first-class architectural concern, not an open question.

#### Moderate

**M1: Compensation/Saga Pattern Underspecified**

The `compensate` failure mode is mentioned but not detailed. Temporal's saga pattern requires:
- Explicit compensation activities defined per forward step.
- Compensation runs in reverse order of completed steps.
- Compensation activities must execute in non-cancellable scopes.

Recommended shape:

```yaml
- name: provision-resource
  type: action
  action: cloud.provision
  compensation:
    action: cloud.deprovision
    with:
      resourceId: ${context.provision-resource.id}
```

**M2: Cancellation Semantics Incomplete**

"Propagate cancel to running branches" is mentioned, but Temporal cancellation is more nuanced:
- Cancellation propagates through `CancellationScope` hierarchies.
- Cleanup/compensation must run in non-cancellable scopes (otherwise they get cancelled too).
- Child workflows have a `ParentClosePolicy` (`TERMINATE`, `REQUEST_CANCEL`, `ABANDON`).

Action: define per-step cancellation behavior, parent close policy for child workflow steps, and what happens to running agent calls on cancellation.

**M3: No Search Attributes**

Temporal search attributes enable querying and filtering workflow executions. For the observability goals, define custom search attributes like `kagentTemplateName`, `kagentRunName`, `kagentNamespace`. This enables queries such as:

```
kagentTemplateName = 'build-and-test' AND ExecutionStatus = 'Running'
```

#### Minor

**m1: Side Effects Not Addressed**

The generated workflow code may need non-deterministic values (UUIDs, timestamps). Temporal provides `SideEffect` and `MutableSideEffect` for this. The compiler must ensure any generated code that needs randomness or time uses these APIs, not direct calls.

**m2: Context Model vs Temporal Data Passing**

The `${context.*}` shared key-value store is a convenient abstraction, but Temporal passes data between activities as return values stored in event history (not a separate store). The compiler should map context reads to activity result references. Large payloads in context will bloat event history.

Action: add payload size limits/warnings and consider external storage (S3, blob) for large outputs, passing only references through Temporal.

### Summary Scorecard

| Area | Status | Priority |
|---|---|---|
| Orchestration / activity separation | Good | — |
| DAG model | Good | — |
| Child workflow use | Good | — |
| Retry policy completeness | Needs work | Critical |
| Timeout model (4 types) | Needs work | Critical |
| Heartbeat support | Missing | Critical |
| Task queue routing | Missing | Important |
| Workflow ID strategy | Missing | Important |
| Event history limits / Continue-As-New | Missing | Important |
| Versioning for in-flight runs | Missing (open question) | Important |
| Saga / compensation detail | Underspecified | Moderate |
| Cancellation scopes | Underspecified | Moderate |
| Search attributes | Missing | Moderate |
| Side effects handling | Missing | Minor |
| Context / payload size limits | Missing | Minor |

### Conclusion

The architectural direction is sound — declarative authoring compiled to Temporal primitives is a valid and useful pattern. The main gaps are in the **fidelity of the Temporal mapping**: the spec abstracts away too many Temporal-specific knobs that users will need, especially around timeouts, heartbeats, retries, and versioning. Addressing the Critical and Important items before v1 will prevent breaking schema changes later.
