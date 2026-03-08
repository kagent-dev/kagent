# EP-2003: AgentCronJob — Scheduled AI Agent Execution

* Status: **Implemented**
* Spec: [specs/ai-cron-jobs](../specs/ai-cron-jobs/)

## Background

New Kubernetes CRD (`AgentCronJob`, `kagent.dev/v1alpha2`) that schedules AI agent prompt execution on a cron schedule. References an existing Agent CR and sends a static prompt at each tick via the kagent HTTP API, storing results in sessions.

## Motivation

Users need to run agent tasks on recurring schedules (e.g., daily cluster health checks, periodic report generation) without manual intervention.

### Goals

- Minimal CRD spec: `schedule` + `prompt` + `agentRef`
- RequeueAfter-based scheduling (no in-memory cron library, survives restarts)
- Reuse existing session/task/event DB models
- HTTP server CRUD at `/api/cronjobs`
- UI page for listing, creating, editing, and deleting cron jobs

### Non-Goals

- Complex scheduling (e.g., dependencies between jobs)
- Parameterized prompts with templating
- Job history retention policies

## Implementation Details

- **CRD:** `go/api/v1alpha2/agentcronjob_types.go`
- **Controller:** Reconciler with RequeueAfter scheduling using `robfig/cron/v3` for parsing
- **API:** `go/core/internal/httpserver/handlers/cronjobs.go`
- **UI:** `ui/src/app/cronjobs/page.tsx`
- **Status fields:** `lastRunTime`, `nextRunTime`, `lastRunResult`, `lastRunMessage`, `lastSessionID`

### Test Plan

- E2E tests for CRD lifecycle
- Unit tests for schedule parsing and next-run calculation
