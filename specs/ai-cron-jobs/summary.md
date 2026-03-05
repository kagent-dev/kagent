# AgentCronJob — Project Summary

## Artifacts

| File | Description |
|------|-------------|
| `specs/ai-cron-jobs/rough-idea.md` | Original concept |
| `specs/ai-cron-jobs/requirements.md` | 11 Q&A pairs defining scope and constraints |
| `specs/ai-cron-jobs/research/crd-types-patterns.md` | Existing v1alpha2 CRD patterns |
| `specs/ai-cron-jobs/research/http-server-api.md` | Session creation and A2A invocation flow |
| `specs/ai-cron-jobs/research/controller-patterns.md` | Shared reconciler and controller registration |
| `specs/ai-cron-jobs/research/database-models.md` | Session/task/event models (no new models needed) |
| `specs/ai-cron-jobs/research/ui-placeholder.md` | Existing UI placeholder and CRUD patterns |
| `specs/ai-cron-jobs/design.md` | Full design: CRD types, controller, HTTP API, UI, error handling, acceptance criteria |
| `specs/ai-cron-jobs/plan.md` | 7-step incremental implementation plan |

## Overview

**AgentCronJob** is a new Kubernetes CRD (`kagent.dev/v1alpha2`) that schedules AI agent prompt execution on a cron schedule. It references an existing Agent CR, sends a static prompt at each tick via the kagent HTTP server API (same path as the UI), and stores results in sessions.

Key design decisions:
- **Minimal spec:** schedule + prompt + agentRef (no concurrency policy, suspend, etc.)
- **RequeueAfter scheduling:** no in-memory cron library, survives restarts
- **No new DB models:** reuses sessions/tasks/events
- **HTTP server CRUD:** `/api/cronjobs` backs the existing UI placeholder
- **Error handling:** failed runs set status, retry on next tick

## Suggested Next Steps

1. **Implement** — Follow the 7-step plan in `plan.md`. Steps 2 and 3-5 can be parallelized.
2. **Review dependencies** — Confirm `robfig/cron/v3` is acceptable; it's the standard Go cron library.
3. **Consider future enhancements** — Concurrency policy, suspend/resume, prompt templating, execution timeout (all deferred by design).
