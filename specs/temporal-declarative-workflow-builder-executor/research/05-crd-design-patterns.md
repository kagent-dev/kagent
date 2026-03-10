# Research: CRD Design Patterns for Template + Run Two-Resource Models

## 1. Existing Two-Resource CRD Patterns in Kubernetes

### 1.1 Tekton: Task/TaskRun, Pipeline/PipelineRun

Tekton uses a strict two-resource separation across two levels:

**Task level:**
- `Task` defines steps (containers) with inputs/outputs. Immutable after creation.
- `TaskRun` instantiates a Task with concrete parameter values, workspace bindings, and a service account. Creates a Pod.

**Pipeline level:**
- `Pipeline` defines a DAG of `PipelineTask` entries. Each entry references a Task (via `taskRef` or inline `taskSpec`) and declares ordering via `runAfter` and implicit data dependencies (result references).
- `PipelineRun` instantiates a Pipeline, binding parameters, workspaces, and optionally overriding per-task settings via `taskRunSpecs`.

**Key design decisions:**
- `PipelineRun.spec.pipelineRef` or inline `pipelineSpec` — supports both named reference and inline definition.
- The resolved `PipelineSpec` is snapshot-stored in `PipelineRunStatus.pipelineSpec` so that subsequent template edits do not affect in-flight runs.
- `finally` tasks: a dedicated list of tasks that run after all DAG tasks complete (regardless of success/failure). Analogous to try/finally semantics.
- Conditional execution: `when` expressions on each `PipelineTask` allow skipping based on parameter values or prior results.
- `PipelineTask.onError`: `"stopAndFail"` (default) or `"continue"` per task.
- `PipelineTask.retries`: integer retry count per task.

### 1.2 Argo Workflows: WorkflowTemplate/Workflow

Argo uses a single `Workflow` CRD for execution, with `WorkflowTemplate` and `ClusterWorkflowTemplate` for reusable definitions.

**Template level:**
- `WorkflowTemplate` defines `spec.templates` (step/DAG/container templates) and `spec.arguments` (parameters with defaults).
- Templates can be composed: a template can reference another template via `templateRef`.

**Run level:**
- `Workflow` is both the definition and the execution resource. When used with templates, `spec.workflowTemplateRef` points to a `WorkflowTemplate`.
- `Workflow.spec.arguments` provides concrete parameter values at submission time.

**Key design decisions:**
- `DAGTemplate` contains `tasks` where each `DAGTask` has a `dependencies` field (list of task names).
- Argo stores per-node execution state in `status.nodes` — a flat map keyed by node ID, where each `NodeStatus` tracks: `phase`, `type`, `startedAt`, `finishedAt`, `children` (list of child node IDs), `message`, `inputs`, `outputs`.
- The flat node map means the entire execution DAG state is reconstructible from status alone.
- `RetryStrategy` is richer than Tekton: includes `limit`, `retryPolicy` (Always/OnFailure/OnError/OnTransientError), `backoff` (duration, factor, maxDuration), `expression` (CEL-based retry condition).

### 1.3 Kueue and Kubernetes Job/CronJob Patterns

**Kubernetes Job:**
- `Job` is both template and run in one resource (similar to Argo `Workflow`).
- `Job.spec.template` defines the pod template.
- `CronJob` acts as a template that creates `Job` instances on a schedule, with `spec.jobTemplate` containing the template.

**CronJob history retention:**
- `spec.successfulJobsHistoryLimit` (default: 3) — number of successful Jobs to retain.
- `spec.failedJobsHistoryLimit` (default: 1) — number of failed Jobs to retain.
- The CronJob controller garbage-collects excess Jobs based on these limits.

**Job TTL cleanup:**
- `Job.spec.ttlSecondsAfterFinished` — automatic cascading deletion after completion.
- Timer starts when Job status transitions to Complete or Failed.
- Stable since Kubernetes 1.23.

### 1.4 Summary: How They Separate Definition from Execution

| Project | Template Resource | Run Resource | Snapshot | Inline Support |
|---------|------------------|-------------|----------|----------------|
| Tekton | Pipeline, Task | PipelineRun, TaskRun | Resolved spec stored in run status | Yes (pipelineSpec, taskSpec) |
| Argo | WorkflowTemplate | Workflow | No separate snapshot; template ref is resolved at submission | Yes (inline templates) |
| K8s CronJob | CronJob.spec.jobTemplate | Job | Job is a copy of the template | N/A |
| **Proposed** | WorkflowTemplate | WorkflowRun | Should snapshot resolved spec | Recommended |

---

## 2. Status Reporting Patterns

### 2.1 Conditions vs Phase-Based Status

**Tekton approach — Conditions only (no phase field):**
- Uses a single `Succeeded` condition (from knative `duckv1.Status`).
- Condition status: `True`, `False`, `Unknown`.
- Condition reason encodes the phase-like state: `Started`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `PipelineRunTimeout`, `StoppedRunFinally`, etc.
- This follows the Kubernetes API conventions recommendation: prefer conditions over phase fields.

**Argo approach — Phase + Conditions:**
- `status.phase`: enum (`Pending`, `Running`, `Succeeded`, `Failed`, `Error`).
- `status.conditions`: additional conditions for specific concerns.
- The phase field provides a simple top-level summary; conditions provide detail.

**Kubernetes API conventions (from sig-architecture):**
- Phase fields are discouraged for new APIs because they create a state machine that is hard to evolve.
- Conditions are preferred because they are independently settable and additive.
- A resource can have multiple conditions true simultaneously (e.g., `Ready=True`, `Progressing=True`).

**Recommendation for kagent:** Follow the conditions-only pattern (like Tekton and existing kagent CRDs). Kagent already uses `metav1.Condition` with types like `Accepted` and `Ready`. Add a `phase`-like summary only as a printer column derived from conditions, not as a first-class status field.

### 2.2 Per-Step Status Within Parent Resource

**Tekton PipelineRun:**
- `status.childReferences`: list of `ChildStatusReference` structs containing TaskRun name, PipelineTask name, and when-expression results.
- Actual per-TaskRun status lives on the TaskRun resources themselves.
- `status.skippedTasks`: list of tasks that were skipped (with reason).
- Per-step (container) status is on TaskRun: `status.steps[]` with embedded `corev1.ContainerState`.

**Argo Workflow:**
- `status.nodes`: flat `map[string]NodeStatus` keyed by node ID.
- Each `NodeStatus` has: `id`, `name`, `displayName`, `type` (Pod/DAG/Steps/Retry/Skipped), `phase`, `startedAt`, `finishedAt`, `message`, `children`, `inputs`, `outputs`, `templateName`.
- This embeds the entire execution tree in the parent resource's status.

**Trade-offs:**

| Approach | Pros | Cons |
|----------|------|------|
| Child resources (Tekton) | Each run is a real K8s resource; works with RBAC, events, watches | More resources to manage; status aggregation requires multiple GETs |
| Embedded node map (Argo) | Single GET returns full execution graph; simpler queries | Status can grow very large; etcd size limits (~1.5MB per object) |

**Recommendation for kagent:** Use an embedded step status list (similar to Argo's node map but as a list, not a map) within `WorkflowRunStatus`. Reasons:
1. WorkflowRun steps map to Temporal activities, not to Kubernetes resources — there are no child K8s resources to reference.
2. Temporal is the source of truth for detailed execution state; the CRD status is a synchronized summary.
3. A list of `StepStatus` structs is simpler and sufficient for the kagent UI.
4. If step count is bounded (e.g., max 100 steps per template), size is manageable.

### 2.3 Representing DAG Execution Progress

**Proposed StepStatus shape:**
```go
type StepStatus struct {
    Name           string       `json:"name"`
    Phase          StepPhase    `json:"phase"`            // Pending, Running, Succeeded, Failed, Skipped
    StartTime      *metav1.Time `json:"startTime,omitempty"`
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
    Message        string       `json:"message,omitempty"` // error or summary
    Retries        int32        `json:"retries,omitempty"`
    // For agent steps: the session ID created
    SessionID      string       `json:"sessionID,omitempty"`
}
```

The overall DAG progress can be derived: count steps by phase. The UI can reconstruct the DAG shape from the `WorkflowTemplate.spec.steps[].dependsOn` field and overlay `StepStatus` entries.

---

## 3. Run History and Retention

### 3.1 Patterns From Existing Projects

**Argo Workflows — TTLStrategy:**
```yaml
spec:
  ttlStrategy:
    secondsAfterCompletion: 3600   # delete 1h after any completion
    secondsAfterSuccess: 86400     # delete 24h after success
    secondsAfterFailure: 172800    # delete 48h after failure
```
- Attached to the Workflow (run) resource itself.
- A controller watches completed workflows and deletes them after TTL expires.
- Different TTLs for success vs failure is useful (keep failures longer for debugging).

**Kubernetes Job — ttlSecondsAfterFinished:**
- Single TTL field on the Job spec.
- Built-in TTL controller handles cleanup.

**Kubernetes CronJob — History limits:**
- `successfulJobsHistoryLimit` / `failedJobsHistoryLimit` on the parent (template) resource.
- The CronJob controller garbage-collects excess child Jobs.

**Tekton — No built-in retention:**
- Tekton does not have built-in TTL or history limits.
- Relies on external tools (Tekton Results, custom pruning CronJobs) for cleanup.
- This is widely considered a gap in Tekton's design.

### 3.2 Recommended Approach for Kagent

Combine both patterns — TTL on runs and history limits on templates:

**On WorkflowTemplate (template-level retention policy):**
```go
type RetentionPolicy struct {
    // Maximum number of successful runs to retain per template.
    // Oldest are deleted first. Default: 10.
    // +optional
    SuccessfulRunsHistoryLimit *int32 `json:"successfulRunsHistoryLimit,omitempty"`
    // Maximum number of failed runs to retain per template.
    // Oldest are deleted first. Default: 5.
    // +optional
    FailedRunsHistoryLimit *int32 `json:"failedRunsHistoryLimit,omitempty"`
}
```

**On WorkflowRun (run-level TTL):**
```go
type WorkflowRunSpec struct {
    // ...
    // TTLSecondsAfterFinished controls automatic deletion of the run
    // after it completes. If not set, the template's retention policy applies.
    // +optional
    TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}
```

The controller should:
1. After a run completes, check run-level TTL first.
2. If no run-level TTL, enforce template-level history limits.
3. Both mechanisms can coexist (TTL takes precedence for individual runs).

---

## 4. Parameterization

### 4.1 How Templates Define Parameters

**Tekton ParamSpec:**
```go
type ParamSpec struct {
    Name        string       `json:"name"`
    Type        ParamType    `json:"type,omitempty"`   // string, array, object
    Description string       `json:"description,omitempty"`
    Default     *ParamValue  `json:"default,omitempty"`
    Enum        []string     `json:"enum,omitempty"`   // allowed values
    Properties  map[string]PropertySpec `json:"properties,omitempty"` // for object type
}
```

**Argo Parameter:**
```go
type Parameter struct {
    Name    string  `json:"name"`
    Default *string `json:"default,omitempty"`
    Value   *string `json:"value,omitempty"`
    Enum    []string `json:"enum,omitempty"`
    // GlobalName makes the parameter available globally
    GlobalName string `json:"globalName,omitempty"`
}
```

**Key differences:**
- Tekton supports typed parameters (string/array/object) with JSON Schema-like properties for object types.
- Argo parameters are string-only but simpler.
- Both support `default` values and `enum` validation.
- Tekton's `Description` field is useful for UI generation.

### 4.2 How Parameters Are Passed at Run Time

**Tekton:** `PipelineRun.spec.params` is a list of `{name, value}` pairs. Parameters without values in the run fall back to template defaults. Required parameters (no default) cause validation failure if missing.

**Argo:** `Workflow.spec.arguments.parameters` provides values. The submit CLI can also pass `--parameter key=value`.

### 4.3 Recommended Approach for Kagent

```go
// On WorkflowTemplate
type WorkflowTemplateSpec struct {
    // Parameters declares the input parameters for this workflow.
    // +optional
    Params []ParamSpec `json:"params,omitempty"`
    // ...
}

type ParamSpec struct {
    // Name is the parameter name, used in ${params.name} expressions.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^[a-zA-Z_][a-zA-Z0-9_]*$`
    Name string `json:"name"`
    // Description for UI display.
    // +optional
    Description string `json:"description,omitempty"`
    // Type constrains the parameter value.
    // +kubebuilder:validation:Enum=string;number;boolean
    // +kubebuilder:default=string
    // +optional
    Type ParamType `json:"type,omitempty"`
    // Default value. If set, the parameter is optional.
    // +optional
    Default *string `json:"default,omitempty"`
    // Enum restricts the parameter to a set of allowed values.
    // +optional
    Enum []string `json:"enum,omitempty"`
    // Required indicates whether the parameter must be provided.
    // A parameter is required if it has no default value.
    // This field is computed and read-only in the status;
    // it is inferred from the absence of a default.
}

// On WorkflowRun
type WorkflowRunSpec struct {
    // WorkflowTemplateRef is the name of the WorkflowTemplate to execute.
    // Must be in the same namespace.
    // +kubebuilder:validation:Required
    WorkflowTemplateRef string `json:"workflowTemplateRef"`
    // Params provides values for the template's declared parameters.
    // +optional
    Params []Param `json:"params,omitempty"`
}

type Param struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}
```

**Validation rules (webhook or CEL):**
1. All template params without defaults must appear in `WorkflowRun.spec.params`.
2. All params in the run must match a declared param name in the template.
3. Enum values are validated against the template's enum list.
4. Type coercion/validation (e.g., "number" params must parse as numeric).

---

## 5. Kagent Existing CRD Conventions

### 5.1 Patterns Observed in v1alpha2

From analyzing the existing CRDs (`Agent`, `AgentCronJob`, `ModelConfig`, `ModelProviderConfig`, `RemoteMCPServer`):

**API group and versioning:**
- Group: `kagent.dev`
- Version: `v1alpha2` (current), registered via `SchemeBuilder`
- All CRDs use `+kubebuilder:storageversion`

**Struct layout:**
```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:...
// +kubebuilder:storageversion
type ResourceName struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ResourceNameSpec   `json:"spec,omitempty"`
    Status ResourceNameStatus `json:"status,omitempty"`
}
```

**Status pattern:**
- All status structs include `ObservedGeneration int64` and `Conditions []metav1.Condition`.
- Condition types are string constants defined at package level: `AgentConditionTypeAccepted`, `AgentConditionTypeReady`, etc.
- No phase fields — conditions only.

**Validation:**
- Heavy use of `+kubebuilder:validation:XValidation` CEL rules for cross-field validation.
- Enum fields use `+kubebuilder:validation:Enum`.
- Optional fields use `+optional` and pointer types with `omitempty`.
- Defaults via `+kubebuilder:default`.

**References:**
- `TypedReference` (name, namespace, kind, apiGroup) for cross-resource references.
- `TypedLocalReference` (name, kind, apiGroup) for same-namespace references.
- Secret references are string names (same namespace assumed), not full object references.

**Print columns:**
- Key status fields and spec summaries exposed via `+kubebuilder:printcolumn`.

**Registration:**
- Each type pair (Resource + ResourceList) registered in `init()` via `SchemeBuilder.Register()`.

**Naming conventions:**
- CRD types: PascalCase singular (`Agent`, `ModelConfig`)
- Spec/Status suffixes: `AgentSpec`, `AgentStatus`
- Enum types: PascalCase with underscore-separated values (`AgentType_Declarative`)
- Condition type constants: `ResourceConditionTypeReady`

### 5.2 How New CRDs Should Align

New `WorkflowTemplate` and `WorkflowRun` CRDs should follow these conventions:

1. **Same API group and version:** `kagent.dev/v1alpha2`.
2. **Standard struct layout** with TypeMeta, ObjectMeta, Spec, Status.
3. **Status uses `ObservedGeneration` + `[]metav1.Condition`**, no phase field.
4. **CEL validation rules** for cross-field constraints.
5. **Printer columns** for key fields (template name, phase-like derived column, age).
6. **`TypedLocalReference` or string name** for the template reference from run to template (string name is simpler and consistent with `AgentCronJob.spec.agentRef`).
7. **Register in `init()`** via SchemeBuilder.
8. **`+kubebuilder:storageversion`** marker.

### 5.3 AgentCronJob as Closest Analog

`AgentCronJob` is the closest existing pattern to WorkflowTemplate/WorkflowRun — it references an Agent by name and tracks execution status:

```go
type AgentCronJobSpec struct {
    Schedule string `json:"schedule"`
    Prompt   string `json:"prompt"`
    AgentRef string `json:"agentRef"`
}

type AgentCronJobStatus struct {
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    LastRunTime        *metav1.Time       `json:"lastRunTime,omitempty"`
    NextRunTime        *metav1.Time       `json:"nextRunTime,omitempty"`
    LastRunResult      string             `json:"lastRunResult,omitempty"`
    LastRunMessage     string             `json:"lastRunMessage,omitempty"`
    LastSessionID      string             `json:"lastSessionID,omitempty"`
}
```

This confirms kagent's preference for:
- Simple string references (not full TypedReference) for same-namespace resources.
- Execution metadata directly in status (timestamps, result, session ID).
- Condition-based status reporting.

---

## 6. Ownership and Lifecycle

### 6.1 Owner References Between Template and Run

**Tekton:** `PipelineRun` does NOT set an owner reference to `Pipeline`. They are independent resources. Deleting a Pipeline does not cascade-delete its PipelineRuns.

**Argo:** `Workflow` does NOT set an owner reference to `WorkflowTemplate`. Same rationale — templates are reusable definitions, runs are independent executions.

**Kubernetes CronJob/Job:** `Job` created by `CronJob` DOES have an owner reference to the CronJob. This enables cascading deletion and garbage collection.

**Key consideration:** Owner references cause cascading deletion. For workflow systems:
- Deleting a template should NOT automatically delete all historical runs (users want to inspect past runs).
- However, orphaned runs (template deleted) should eventually be cleaned up.

**Recommendation:** Do NOT set owner reference from `WorkflowRun` to `WorkflowTemplate`. Instead:
- Add a label `kagent.dev/workflow-template: <template-name>` on WorkflowRun for efficient listing.
- Use the retention policy (history limits + TTL) for cleanup.
- Optionally add a finalizer on `WorkflowTemplate` that warns or blocks deletion if active runs exist.

### 6.2 Finalizers

**Tekton:** PipelineRun uses finalizers to ensure child TaskRuns are cleaned up before the PipelineRun is deleted.

**Argo:** Workflow uses finalizers for artifact garbage collection.

**For kagent WorkflowRun:**
- A finalizer should cancel the corresponding Temporal workflow execution when a WorkflowRun is deleted.
- Pattern: `kagent.dev/temporal-cleanup`
- The controller removes the finalizer after confirming the Temporal workflow is terminated.

**For kagent WorkflowTemplate:**
- Optional: a finalizer that checks for active (non-terminal) WorkflowRuns before allowing deletion.
- Or: allow deletion but mark orphaned runs as `Cancelled` with a message indicating the template was deleted.

### 6.3 Immutability of Templates vs Versioning

**Tekton:** Pipeline and Task resources are effectively immutable by convention — PipelineRun snapshots the resolved spec at creation time. Editing a Pipeline does not affect in-flight runs.

**Argo:** WorkflowTemplate can be updated freely. Running workflows that were submitted with a template ref already have a resolved copy. New submissions get the new version.

**Temporal-specific concern (from rough-idea.md review):**
Temporal replays workflow history using the current workflow code. If the template changes mid-run, the generated Temporal workflow code changes, causing non-determinism errors. This is a critical architectural constraint.

**Recommendation:**
1. `WorkflowTemplate` should be mutable (users can update it).
2. `WorkflowRun` must snapshot the resolved template spec at creation time (store in `status.resolvedSpec` or `spec.resolvedSpec`).
3. The Temporal workflow ID should include a template version hash to ensure code/version alignment.
4. In-flight runs are never affected by template changes — they use their snapshot.
5. Consider adding `status.templateGeneration` to WorkflowRun to track which generation of the template was used.

### 6.4 Lifecycle State Machine

```
WorkflowRun lifecycle:

  Created ──> Pending ──> Running ──> Succeeded
                 │            │
                 │            ├──> Failed
                 │            │
                 │            └──> Cancelled
                 │
                 └──> Failed (validation error)
```

Represented as conditions:
- `Accepted` (True/False) — template reference resolved, params validated, Temporal workflow submitted.
- `Running` (True/False) — Temporal workflow is actively executing.
- `Succeeded` (True/False) — terminal success.

The derived "phase" for printer columns: check conditions in order: `Succeeded=True` -> "Succeeded", `Succeeded=False` -> check reason for "Failed"/"Cancelled", `Running=True` -> "Running", `Accepted=False` -> "Invalid", else "Pending".

---

## 7. Summary of Recommendations for Kagent WorkflowTemplate/WorkflowRun

| Design Decision | Recommendation | Precedent |
|----------------|---------------|-----------|
| API group/version | `kagent.dev/v1alpha2` | All existing kagent CRDs |
| Template reference | String name (`workflowTemplateRef`) | `AgentCronJob.spec.agentRef` |
| Template snapshot | Store resolved spec in run status | Tekton PipelineRun |
| Status model | Conditions only, no phase field | Kagent convention, K8s API conventions |
| Per-step status | Embedded `[]StepStatus` in run status | Argo node map (simplified) |
| Parameters | `ParamSpec` on template, `Param` on run | Tekton ParamSpec |
| Param validation | CEL rules + webhook | Kagent convention |
| Retention — history | `successfulRunsHistoryLimit` / `failedRunsHistoryLimit` on template | K8s CronJob |
| Retention — TTL | `ttlSecondsAfterFinished` on run | Argo TTLStrategy, K8s Job |
| Owner references | None (use labels instead) | Tekton, Argo |
| Finalizer on run | `kagent.dev/temporal-cleanup` for Temporal workflow cancellation | Tekton, Argo |
| Template mutability | Mutable, but runs snapshot at creation | Tekton, Argo |
| Run immutability | Spec is immutable after creation | Tekton TaskRun/PipelineRun |
| Condition types | `Accepted`, `Running`, `Succeeded` | Kagent existing patterns |
| Labels on run | `kagent.dev/workflow-template: <name>` | Standard K8s pattern |
