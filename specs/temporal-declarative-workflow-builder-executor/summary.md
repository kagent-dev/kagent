# Summary: Temporal Declarative Workflow Builder & Executor

**Date:** 2026-03-10
**Status:** Design Complete, Implementation Plan Approved

---

## Artifacts

| File | Description |
|------|-------------|
| `rough-idea.md` | Initial concept with goals, non-goals, example YAML, and Temporal best-practices audit |
| `requirements.md` | Q&A record (requirements captured directly in rough idea and design) |
| `research/01-existing-solutions.md` | Temporal DSL sample, Conductor, Hatchet, Zigflow, Kestra, Dagu, GraphAI — patterns and anti-patterns |
| `research/02-kagent-temporal-integration.md` | Current kagent Temporal architecture: workflows, activities, signals, NATS, reusable components |
| `research/03-dag-execution-temporal.md` | DAG execution in Temporal Go SDK: fan-out/fan-in, dynamic activities, topological sort, history limits |
| `research/04-declarative-dsl-design.md` | DSL patterns from Argo, Tekton, GitHub Actions, Dagster — interpolation, output mapping, retry schemas |
| `research/05-crd-design-patterns.md` | Template+Run CRD model from Tekton/Argo, status reporting, retention, parameterization, kagent conventions |
| `research/06-hatchet-reuse-feasibility.md` | Hatchet analysis — not embeddable, no YAML, don't adopt |
| `design.md` | Complete design: CRD types, DAG compiler, expression engine, Temporal interpreter, controllers, API, testing |
| `plan.md` | 12-step incremental implementation plan with dependency graph and critical path |

## Overview

A declarative workflow system for kagent that compiles YAML CRD definitions (`WorkflowTemplate` + `WorkflowRun`) into Temporal workflow executions. Users define DAGs of `action` and `agent` steps with explicit dependencies, typed parameters, and Temporal-native retry/timeout policies. A generic interpreter workflow executes the plan at runtime, avoiding Temporal versioning/replay issues entirely.

## Key Design Decisions

- **Generic interpreter** (not code-gen) — the DAGWorkflow reads the execution plan from input, so template changes never affect in-flight runs
- **Event-driven DAG** — `workflow.Await` per step for maximum parallelism
- **`${{ }}` interpolation** — GitHub Actions style, avoids Helm/shell collisions
- **Template snapshot** — run captures resolved spec at creation, immutable thereafter
- **200-step cap** — uses ~800 Temporal events, well within 51,200 limit
- **Two step types in v1** — `action` (Temporal activity) and `agent` (child workflow to kagent agent)

## Next Steps

1. **Implement** — follow `plan.md` steps 1–12 (critical path: CRDs → compiler → expressions → DAGWorkflow → run controller → status syncer → E2E)
2. **Parallel work** — action registry, agent step, template controller, HTTP API, retention can proceed in parallel after core path
3. **v2 planning** — loops/conditionals, compensation/saga, container image steps, visual designer
