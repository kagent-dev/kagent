# Implementation Plan: Temporal Declarative Workflow Builder & Executor

**Date:** 2026-03-10
**Design:** [design.md](design.md)

---

## Checklist

- [ ] Step 1: CRD Types and Code Generation
- [ ] Step 2: DAG Compiler and Validation
- [ ] Step 3: Expression Interpolation Engine
- [ ] Step 4: DAGWorkflow Temporal Interpreter
- [ ] Step 5: Action Activity Framework
- [ ] Step 6: Agent Step (Child Workflow)
- [ ] Step 7: WorkflowTemplate Controller
- [ ] Step 8: WorkflowRun Controller
- [ ] Step 9: Status Syncer
- [ ] Step 10: HTTP API Endpoints
- [ ] Step 11: Retention Controller
- [ ] Step 12: E2E Tests and Examples

---

## Step 1: CRD Types and Code Generation

**Objective:** Define WorkflowTemplate and WorkflowRun CRD types in `go/api/v1alpha2/` and generate manifests.

**Implementation guidance:**
- Create `go/api/v1alpha2/workflow_types.go` with all types from design section 4.1–4.2: `WorkflowTemplate`, `WorkflowTemplateSpec`, `WorkflowTemplateStatus`, `WorkflowRun`, `WorkflowRunSpec`, `WorkflowRunStatus`, `StepSpec`, `StepOutput`, `StepPolicy`, `RetryPolicy`, `TimeoutPolicy`, `StepPolicyDefaults`, `RetentionPolicy`, `ParamSpec`, `Param`, `StepStatus`, `StepPhase` constants.
- Add kubebuilder markers: `+kubebuilder:object:root`, `+kubebuilder:subresource:status`, `+kubebuilder:storageversion`, `+kubebuilder:printcolumn`, validation annotations (`Enum`, `Pattern`, `MinItems`, `MaxItems`, `Required`).
- Register types in `init()` via `SchemeBuilder.Register`.
- Create `WorkflowTemplateList` and `WorkflowRunList` types.
- Run `make -C go generate` to produce deepcopy, CRD manifests, and RBAC.
- Update Helm CRD chart (`helm/kagent-crds/`) with generated manifests.

**Test requirements:**
- `go generate` succeeds without errors.
- CRD manifests pass `kubectl apply --dry-run=server`.
- Types compile and deepcopy methods are generated.

**Integration notes:** No runtime dependencies yet. Pure type definitions.

**Demo:** `kubectl apply -f` the generated CRD manifests. `kubectl explain workflowtemplate.spec.steps` shows the schema.

---

## Step 2: DAG Compiler and Validation

**Objective:** Build the compiler that validates WorkflowTemplateSpec and produces an ExecutionPlan.

**Implementation guidance:**
- Create `go/core/internal/compiler/dag.go` with `DAGCompiler` struct.
- Implement `Validate(spec *WorkflowTemplateSpec) error`:
  - No duplicate step names.
  - All `dependsOn` references resolve to existing step names.
  - Cycle detection via topological sort (Kahn's algorithm on the dependency graph).
  - `action` steps require `action` field; `agent` steps require `agentRef`.
  - Step count <= 200.
- Implement `Compile(spec *WorkflowTemplateSpec, params map[string]string) (*ExecutionPlan, error)`:
  - Validate params against ParamSpec (required params present, enum checks, type coercion).
  - Merge step-level policies with template defaults.
  - Produce `ExecutionPlan` struct (JSON-serializable, passed as Temporal workflow input).
- Create `go/core/internal/compiler/dag_test.go` with table-driven tests.

**Test requirements:**
- Cycle detection: triangle A→B→C→A returns error.
- Missing dependency: step references nonexistent name returns error.
- Duplicate names: returns error.
- Valid DAG: compiles without error, produces correct plan.
- Param validation: missing required param, invalid enum value, type mismatch.
- Policy merging: step policy overrides defaults, defaults apply when step omits.

**Integration notes:** No Temporal dependency. Pure Go logic.

**Demo:** Unit tests pass. A test prints the JSON ExecutionPlan for the `build-and-test` example from the design doc.

---

## Step 3: Expression Interpolation Engine

**Objective:** Build the `${{ }}` expression resolver for params and context references.

**Implementation guidance:**
- Create `go/core/internal/compiler/expr.go` with:
  - `ResolveExpression(expr string, params map[string]string, ctx *WorkflowContext) (string, error)`
  - `ValidateExpressions(spec *WorkflowTemplateSpec) []error` — static check that all `${{ params.* }}` refs exist in ParamSpec.
  - `ExtractExpressions(s string) []Expression` — parse all `${{ }}` tokens from a string.
- Expression types supported:
  - `${{ params.name }}` — lookup in params map.
  - `${{ context.stepName.field }}` — JSON path into step output.
  - `${{ context.globalKey }}` — lookup in globals (from `output.keys`).
  - `${{ workflow.name }}`, `${{ workflow.namespace }}`, `${{ workflow.runName }}` — metadata.
- Literal escape: `$${{ }}` produces `${{ }}`.
- Error on unresolved references (unknown param name, missing context key at runtime).
- Create `go/core/internal/compiler/expr_test.go`.

**Test requirements:**
- Simple param substitution: `${{ params.url }}` → resolved value.
- Nested field access: `${{ context.checkout.path }}` → JSON field extraction.
- Multiple expressions in one string: `"${{ params.a }}-${{ params.b }}"`.
- Escape: `$${{ not.resolved }}` → literal `${{ not.resolved }}`.
- Unknown param: returns error at validation time.
- Unknown context key: returns error at runtime.
- No expressions: passthrough.

**Integration notes:** Used by DAG compiler (validation) and DAGWorkflow (runtime resolution).

**Demo:** Unit tests pass covering all expression patterns.

---

## Step 4: DAGWorkflow Temporal Interpreter

**Objective:** Implement the generic `DAGWorkflow` that interprets an ExecutionPlan at runtime.

**Implementation guidance:**
- Create `go/core/internal/temporal/workflow/dag_workflow.go`.
- Register `DAGWorkflow(ctx workflow.Context, plan *ExecutionPlan) (*DAGResult, error)`.
- Implement event-driven DAG execution per design section 7.1:
  - Initialize `WorkflowContext` from plan params.
  - Launch one `workflow.Go` goroutine per step.
  - Each goroutine calls `workflow.Await` until all `dependsOn` are in `completed` set.
  - Skip step if any `stop`-mode dependency is in `failed` set.
  - Resolve step inputs via expression engine.
  - Dispatch to `ActionActivity` or `executeAgent` based on step type.
  - Store outputs in `WorkflowContext`.
  - Send result to a shared `workflow.Channel`.
  - Collect all results, determine overall status.
- Implement a workflow query handler (`dag-status`) that returns current step statuses (for the status syncer).
- Add Temporal search attributes: `KagentWorkflowTemplate`, `KagentWorkflowRun`, `KagentNamespace`.
- Create `go/core/internal/temporal/workflow/dag_workflow_test.go` using Temporal's test environment.

**Test requirements:**
- Linear DAG (A→B→C): steps execute in order.
- Parallel DAG (A→[B,C]→D): B and C run concurrently.
- Fail-fast: B fails with `onFailure: stop`, C (depends on B) is skipped.
- Continue-on-error: B fails with `onFailure: continue`, C still runs.
- Context data flow: A output available to B via `${{ context.A.field }}`.
- Cancellation: workflow context cancelled, all goroutines exit.

**Integration notes:** Depends on Step 2 (ExecutionPlan types) and Step 3 (expression resolution). Activities are mocked in tests.

**Demo:** Temporal test suite passes. Workflow executes a 5-step DAG with parallel branches using mock activities.

---

## Step 5: Action Activity Framework

**Objective:** Implement the ActionActivity and registry for dispatching step actions.

**Implementation guidance:**
- Create `go/core/internal/temporal/workflow/action_activity.go`:
  - `ActionActivity(ctx context.Context, req *ActionRequest) (*ActionResult, error)` — looks up handler by name, executes, returns result.
  - `ActionRequest` struct: `Action string`, `Inputs map[string]string`.
  - `ActionResult` struct: `Output json.RawMessage`, `Error string`.
- Create `go/core/internal/temporal/workflow/action_registry.go`:
  - `ActionRegistry` struct with `Register(name, handler)`, `Get(name)`.
  - `ActionHandler` interface: `Execute(ctx context.Context, inputs map[string]string) (*ActionResult, error)`.
- Built-in handlers for v1:
  - `http.request` — makes HTTP call, returns response body.
  - `noop` — returns inputs as outputs (for testing/placeholder).
- Activity options derived from step policy: map `RetryPolicy` and `TimeoutPolicy` to `workflow.ActivityOptions`.
- Create tests with mock handlers.

**Test requirements:**
- Known action dispatches to correct handler.
- Unknown action returns `NonRetryableApplicationError`.
- Activity options correctly map retry and timeout policies.
- `http.request` handler makes call and returns result.

**Integration notes:** Registered on the DAG worker. Step 4 calls this activity.

**Demo:** Action activity executes `http.request` against a local test server.

---

## Step 6: Agent Step (Child Workflow)

**Objective:** Implement agent step execution via child workflow to existing kagent agents.

**Implementation guidance:**
- Create `go/core/internal/temporal/workflow/agent_step.go`:
  - `executeAgent(ctx workflow.Context, step ExecutionStep, inputs map[string]string, wfCtx *WorkflowContext) (*StepResult, error)`.
  - Render prompt template with expression engine.
  - Build `ChildWorkflowOptions`: workflow ID = `{dagWorkflowID}:agent:{stepName}`, task queue = `agentRef` (agent name), `ParentClosePolicy: REQUEST_CANCEL`.
  - Call `workflow.ExecuteChildWorkflow` targeting the existing `AgentExecutionWorkflow`.
  - Map agent response to `StepResult` using `output.keys` configuration.
- Handle agent timeout and errors.
- Create `go/core/internal/temporal/workflow/agent_step_test.go`.

**Test requirements:**
- Child workflow started with correct task queue (agent name).
- Prompt rendered with context variables.
- Output keys mapped correctly from agent response.
- Agent timeout propagates as step failure.
- Cancellation propagates to child workflow.

**Integration notes:** Depends on existing `AgentExecutionWorkflow` in `go/adk/pkg/temporal/`. The DAG worker doesn't need agent dependencies — it just starts child workflows on agent task queues.

**Demo:** Agent step in test environment sends prompt to mock agent, receives response, maps output to context.

---

## Step 7: WorkflowTemplate Controller

**Objective:** Implement the controller that validates WorkflowTemplate CRDs on create/update.

**Implementation guidance:**
- Create `go/core/internal/controller/workflowtemplate_controller.go`:
  - `WorkflowTemplateReconciler` struct with `client.Client`, `Scheme`, `Compiler *compiler.DAGCompiler`.
  - `SetupWithManager`: watch `WorkflowTemplate`, filter on generation changes.
  - `Reconcile`: call `compiler.Validate(spec)`, update `status.validated`, `status.stepCount`, conditions.
  - Condition `Accepted`: True if validation passes, False with reason (`CycleDetected`, `InvalidReference`, `DuplicateStepName`, etc.).
- Register in controller manager startup (`go/core/cmd/controller/main.go`).
- Add RBAC markers for WorkflowTemplate resources.
- Run `make -C go generate` for RBAC manifests.
- Create `go/core/internal/controller/workflowtemplate_controller_test.go`.

**Test requirements:**
- Valid template: `Accepted=True`, `validated=true`, correct `stepCount`.
- Invalid template (cycle): `Accepted=False`, reason=`CycleDetected`.
- Template update: re-validates, updates status.

**Integration notes:** Depends on Step 1 (CRD types) and Step 2 (compiler).

**Demo:** Apply a WorkflowTemplate YAML. `kubectl get workflowtemplates` shows validation status.

---

## Step 8: WorkflowRun Controller

**Objective:** Implement the controller that manages WorkflowRun lifecycle: validate, snapshot, submit, finalize.

**Implementation guidance:**
- Create `go/core/internal/controller/workflowrun_controller.go`:
  - `WorkflowRunReconciler` struct with `client.Client`, `Scheme`, `TemporalClient`, `Compiler`.
  - `SetupWithManager`: watch `WorkflowRun`.
  - `Reconcile` flow:
    1. **If not accepted:** Resolve template by name. Validate params. Snapshot template into `status.resolvedSpec`. Set `status.templateGeneration`. Set condition `Accepted=True`. Add finalizer `kagent.dev/temporal-cleanup`.
    2. **If accepted, no Temporal ID:** Compile execution plan. Call `temporalClient.StartWorkflow(DAGWorkflow, plan)`. Store `status.temporalWorkflowID`. Set condition `Running=True`.
    3. **If being deleted:** Cancel Temporal workflow. Remove finalizer.
  - Workflow ID format: `wf-{namespace}-{templateName}-{runName}`.
  - Task queue: `kagent-workflows`.
  - Set Temporal search attributes.
- Register in controller manager.
- Add RBAC markers.
- Create `go/core/internal/controller/workflowrun_controller_test.go`.

**Test requirements:**
- Template not found: `Accepted=False`, reason=`TemplateNotFound`.
- Missing required param: `Accepted=False`, reason=`InvalidParams`.
- Valid run: snapshot stored, Temporal workflow started, `Running=True`.
- Deletion: Temporal workflow cancelled, finalizer removed.
- Idempotent reconciliation: re-reconcile doesn't create duplicate workflows.

**Integration notes:** Depends on Steps 1, 2, 4. Requires Temporal client access in controller.

**Demo:** Apply WorkflowRun YAML. Temporal UI shows the workflow. `kubectl get workflowruns` shows Running status.

---

## Step 9: Status Syncer

**Objective:** Synchronize Temporal workflow state back to WorkflowRun CRD status.

**Implementation guidance:**
- Create `go/core/internal/controller/workflowrun_status_syncer.go`:
  - Background goroutine in the controller manager.
  - Periodically (every 5s) list WorkflowRuns with `Running=True` condition.
  - For each, query Temporal via `DescribeWorkflowExecution` for overall status.
  - Query the `dag-status` query handler for per-step statuses.
  - Update `WorkflowRunStatus`: conditions, phase, steps, startTime, completionTime.
  - On workflow completion: set `Succeeded=True/False`, `Running=False`.
- Use Temporal's `workflow.SetQueryHandler("dag-status", ...)` in DAGWorkflow (Step 4) to expose step states.
- Handle Temporal unavailability gracefully (log, retry next cycle).

**Test requirements:**
- Running workflow: step statuses sync to CRD.
- Completed workflow: conditions updated to Succeeded.
- Failed workflow: conditions updated to Failed with message.
- Temporal down: syncer logs error, retries, doesn't crash.

**Integration notes:** Depends on Steps 4 (query handler) and 8 (WorkflowRun controller). Runs alongside the controller.

**Demo:** Watch `kubectl get workflowruns -w`. Status updates in real-time as steps complete.

---

## Step 10: HTTP API Endpoints

**Objective:** Add REST API endpoints for workflow management.

**Implementation guidance:**
- Create `go/core/internal/httpserver/handlers/workflows.go`:
  - `HandleListWorkflowTemplates` — list templates, optional namespace filter.
  - `HandleGetWorkflowTemplate` — get template by name with full spec.
  - `HandleCreateWorkflowRun` — accepts `{templateRef, params}` JSON, creates WorkflowRun CRD.
  - `HandleListWorkflowRuns` — list runs, filter by template/status/namespace.
  - `HandleGetWorkflowRun` — get run detail with step statuses.
  - `HandleDeleteWorkflowRun` — delete (triggers cancellation via finalizer).
- Register routes in `go/core/internal/httpserver/server.go`.
- Add auth middleware (same as existing endpoints).
- Create `go/core/internal/httpserver/handlers/workflows_test.go`.

**Test requirements:**
- List templates returns correct items.
- Create run with valid params returns 201 with run details.
- Create run with missing params returns 400.
- Get run returns step statuses.
- Delete run returns 200 and triggers deletion.

**Integration notes:** Depends on Steps 1 (CRD types) and 8 (controller creates CRDs). Uses existing K8s client from HTTP server.

**Demo:** `curl` the API endpoints. Create a run via API, watch status via polling.

---

## Step 11: Retention Controller

**Objective:** Implement garbage collection for old WorkflowRuns based on retention policies.

**Implementation guidance:**
- Create `go/core/internal/controller/workflowrun_retention.go`:
  - Runs as periodic reconciliation (every 60s) or triggered by WorkflowRun completion.
  - For each WorkflowTemplate with a retention policy:
    - List completed WorkflowRuns by label `kagent.dev/workflow-template`.
    - Separate into succeeded and failed.
    - Sort by completion time.
    - Delete oldest beyond the history limit.
  - For WorkflowRuns with `ttlSecondsAfterFinished`:
    - Check if TTL has expired.
    - Delete if expired.
- Integrate into the WorkflowRun controller (reconcile on completion) or as a separate periodic controller.

**Test requirements:**
- History limit of 3 with 5 runs: 2 oldest deleted.
- TTL expired: run deleted.
- TTL not expired: run retained.
- No retention policy: no cleanup.

**Integration notes:** Depends on Steps 1, 7, 8. Uses K8s client to list and delete.

**Demo:** Create 5 runs, set history limit to 2. After completion, only 2 most recent runs remain.

---

## Step 12: E2E Tests and Examples

**Objective:** End-to-end tests on a Kind cluster and example workflow templates.

**Implementation guidance:**
- Create `go/core/test/e2e/workflow_test.go` with tests from design section 10:
  - `TestE2EWorkflowSequential` — linear A→B→C.
  - `TestE2EWorkflowParallelDAG` — A→[B,C]→D.
  - `TestE2EWorkflowAgentStep` — agent step calls a test agent.
  - `TestE2EWorkflowFailFast` — step failure skips dependents.
  - `TestE2EWorkflowRetry` — retry policy honored.
  - `TestE2EWorkflowCancellation` — delete run cancels workflow.
  - `TestE2EWorkflowRetention` — history limits enforced.
  - `TestE2EWorkflowAPIEndpoints` — HTTP API CRUD operations.
- Create example YAML files in `examples/workflows/`:
  - `build-and-test.yaml` — CI pipeline (from design doc).
  - `data-pipeline.yaml` — simple ETL: extract → transform → load.
  - `agent-analysis.yaml` — multi-agent analysis workflow.
- Update Helm chart (`helm/kagent/`) to include DAG worker deployment and `kagent-workflows` task queue configuration.
- Update `helm/kagent-crds/` with new CRD manifests.

**Test requirements:**
- All E2E tests pass on Kind cluster with Temporal deployed.
- Example workflows apply cleanly and execute successfully.
- Helm chart deploys with workflow support enabled.

**Integration notes:** Requires all previous steps. Needs Kind cluster, Temporal, NATS, and at least one test agent deployed.

**Demo:** Full end-to-end: apply example workflow template, create run, watch steps execute in parallel, see status in `kubectl` and HTTP API.

---

## Dependency Graph

```
Step 1 (CRD Types)
  |
  +---> Step 2 (DAG Compiler) ---> Step 3 (Expressions)
  |         |                            |
  |         +----------------------------+
  |         |
  |         v
  |     Step 4 (DAGWorkflow) ---> Step 5 (Action Activity)
  |         |                     Step 6 (Agent Step)
  |         |
  |         v
  +---> Step 7 (Template Controller)
  |         |
  |         v
  +---> Step 8 (Run Controller) ---> Step 9 (Status Syncer)
  |                                   Step 10 (HTTP API)
  |                                   Step 11 (Retention)
  |
  +---> Step 12 (E2E + Examples) [depends on all above]
```

**Critical path:** 1 → 2 → 3 → 4 → 8 → 9 → 12

**Parallelizable:** Steps 5, 6, 7 can be built in parallel after Step 2/3. Steps 10, 11 can be built in parallel after Step 8.
