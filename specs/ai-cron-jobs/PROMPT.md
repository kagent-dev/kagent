# AgentCronJob Implementation

## Objective

Implement `AgentCronJob` — a Kubernetes CRD (`kagent.dev/v1alpha2`) that schedules AI agent prompt execution on a cron. Minimal MVP: schedule + prompt + agentRef. Controller triggers runs via the existing HTTP server API, results stored in sessions.

## Key Requirements

- CRD: `AgentCronJob` with spec fields `schedule` (cron), `prompt` (string), `agentRef` (string)
- Status: `lastRunTime`, `nextRunTime`, `lastRunResult`, `lastRunMessage`, `lastSessionID`, conditions (`Accepted`, `Ready`)
- Controller uses `RequeueAfter` with `robfig/cron/v3` for schedule parsing — no in-memory scheduler
- Execution: POST `/api/sessions` to create session, POST `/api/a2a/{ns}/{name}` to send prompt
- On failure: set status Failed, retry on next tick — no immediate requeue
- On restart: recalculate next run from schedule, do NOT retroactively execute missed runs
- HTTP server: CRUD endpoints at `/api/cronjobs` (proxy to K8s API, same pattern as `/api/agents`)
- UI: replace placeholder at `ui/src/app/cronjobs/page.tsx` with list + create/edit form
- No new database models — reuse existing sessions/tasks/events

## Acceptance Criteria

- Given a valid AgentCronJob manifest, when applied, then status shows Accepted=True and nextRunTime populated
- Given a scheduled AgentCronJob, when tick fires, then session is created, prompt sent, status updated with Success + sessionID
- Given an AgentCronJob referencing non-existent agent, when tick fires, then status shows Failed with error message
- Given HTTP server running, when CRUD requests sent to /api/cronjobs, then CRs are created/read/updated/deleted
- Given UI loaded at /cronjobs, then user can list, create, edit, delete cron jobs and see status
- Given controller restarts, then next run recalculated without retroactive execution

## Reference

Full specs in `specs/ai-cron-jobs/` — design.md (architecture, types, error handling), plan.md (7 implementation steps), research/ (codebase patterns).
