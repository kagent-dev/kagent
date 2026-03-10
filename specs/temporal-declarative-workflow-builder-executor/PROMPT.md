# Temporal Declarative Workflow Builder & Executor

## Objective

Implement a declarative workflow system for kagent that compiles YAML CRD definitions into Temporal workflow executions. Users define DAGs via `WorkflowTemplate` and `WorkflowRun` CRDs. A generic Temporal interpreter workflow executes the plan at runtime.

## Key Requirements

- Two CRDs: `WorkflowTemplate` (definition) and `WorkflowRun` (execution) in `kagent.dev/v1alpha2`
- DAG execution with explicit `dependsOn` dependencies and automatic parallelism
- Two step types: `action` (Temporal activity) and `agent` (child workflow to kagent Agent)
- `${{ params.* }}` and `${{ context.* }}` expression interpolation (GitHub Actions style)
- Per-step retry and timeout policies mapped 1:1 to Temporal's RetryPolicy and activity timeouts
- Per-step `onFailure: stop` (default) or `continue` failure modes
- Template snapshot stored in run status at creation (immutable, avoids Temporal replay issues)
- Generic interpreter workflow (not code-gen) — reads ExecutionPlan from workflow input
- Event-driven DAG execution via `workflow.Await` for maximum parallelism
- 200-step cap per template
- Controllers for template validation, run lifecycle, status sync, and retention
- HTTP API for CRUD operations on templates and runs
- Finalizer on WorkflowRun for Temporal workflow cancellation on delete

## Acceptance Criteria

- **Given** a WorkflowTemplate with a dependency cycle, **when** created, **then** `Accepted=False` with reason `CycleDetected`
- **Given** steps A→B→C, **when** run created, **then** steps execute sequentially, run reaches Succeeded
- **Given** A→[B,C]→D, **when** run created, **then** B and C execute concurrently after A, D after both
- **Given** step `type: agent, agentRef: my-agent`, **when** executed, **then** child workflow started on `my-agent` task queue with rendered prompt, output mapped to context
- **Given** `retry.maxAttempts: 3` and activity fails twice then succeeds, **then** step succeeds with `retries: 2`
- **Given** step B with `onFailure: stop` fails, **then** dependent C is Skipped, workflow is Failed
- **Given** step B with `onFailure: continue` fails, **then** dependent C still executes
- **Given** required param missing from WorkflowRun, **then** `Accepted=False`, no Temporal workflow started
- **Given** running WorkflowRun and template updated, **then** run uses its snapshot unaffected
- **Given** WorkflowRun deleted, **then** Temporal workflow cancelled, finalizer removed
- **Given** `successfulRunsHistoryLimit: 3` with 5 runs, **then** 2 oldest deleted
- **Given** step A outputs `{"path": "/src"}` and step B references `${{ context.A.path }}`, **then** resolves to `/src`

## Reference

All design details, CRD type definitions, component interfaces, and research findings are in:

```
specs/temporal-declarative-workflow-builder-executor/
├── design.md    — complete design (CRDs, compiler, interpreter, controllers, API)
├── plan.md      — 12-step implementation plan with dependency graph
└── research/    — 6 research documents (existing solutions, kagent Temporal, DAG patterns, DSL design, CRD patterns, Hatchet)
```

Follow `plan.md` step-by-step. Each step has: objective, implementation guidance, test requirements, integration notes, and demo description.

## Constraints

- Go for all backend code (controllers, compiler, Temporal workflows)
- Follow kagent conventions in CLAUDE.md (error wrapping, table-driven tests, conventional commits)
- CRDs in `go/api/v1alpha2/`, controllers in `go/core/internal/controller/`, Temporal code in `go/core/internal/temporal/workflow/`
- Run `make -C go generate` after CRD type changes
- No loops, conditionals, or Turing-complete logic in the DSL (v1 scope)
- No new infrastructure dependencies — build on existing Temporal integration
