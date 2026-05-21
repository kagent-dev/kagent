# Scheduled Runs

`ScheduledRun` is a kagent CRD that fires an existing `Agent` on a cron schedule
with a fixed prompt. It addresses [issue
#1821](https://github.com/kagent-dev/kagent/issues/1821): users wanted a way to
run agents on a recurring schedule without writing a separate cronjob and
A2A client.

This document describes the end-to-end design, the rationale behind each layer,
the scenarios it covers, and known issues.

---

## Scope

The feature is scoped narrowly to one use case: **trigger an already-deployed
Agent on a cron schedule with a static prompt, and surface the resulting session
in the UI under the user who created the schedule.**

Out of scope (deliberately):

- Building agents — schedules reference existing `Agent` resources, they don't
  define agent behavior themselves.
- Variable / templated prompts.
- Backfill of missed runs while the controller was down.
- Cross-cluster / external triggers.
- Per-run resource quotas.
- A first-class concurrency policy (`Forbid`/`Allow`/`Replace`). Agents already
  isolate runs by session, so two overlapping `ScheduledRun` ticks land in
  separate sessions and don't interleave events. We removed the policy field
  to avoid leaking a Kubernetes `CronJob`-shaped concept that doesn't actually
  apply here. A future iteration may revisit it for resource-budget reasons,
  not correctness.

---

## End-to-End Design

```
┌────────────┐   1. POST /api/scheduledruns        ┌──────────────────────┐
│            │ ────────────────────────────────▶  │  HTTP Handler        │
│   Next.js  │   (X-User-ID: alice@…)              │  scheduledruns.go    │
│     UI     │                                     │                      │
│            │ ◀── 201 (created-by annotation set) │  ─ writes annotation │
└────────────┘                                     │   kagent.dev/        │
      ▲                                            │   created-by=alice@… │
      │                                            └──────────┬───────────┘
      │                                                       │
      │                                                       ▼
      │                                            ┌──────────────────────┐
      │ 5. GET /api/sessions/{id}                  │   Kubernetes API     │
      │    (matches userID)                        │   ScheduledRun CR    │
      │                                            └──────────┬───────────┘
      │                                                       │ watch
      │                                                       ▼
      │                                            ┌──────────────────────┐
      │                                            │ ScheduledRun         │
      │                                            │ Controller           │
      │                                            │  ─ validates TZ      │
      │                                            │  ─ validates cron    │
      │                                            │  ─ checks Agent ref  │
      │                                            │  ─ Accepted=True     │
      │                                            │  ─ scheduler.Update  │
      │                                            └──────────┬───────────┘
      │                                                       │
      │                                                       ▼
      │                              2. cron tick    ┌──────────────────────┐
      │                                              │ ScheduledRunScheduler│
      │                                              │  (Runnable, leader-  │
      │                                              │   elected)           │
      │                                              │                      │
      │                                              │ runOnce():           │
      │                                              │  a. read SR + ann    │
      │                                              │  b. create session   │
      │                                              │     userID = ann     │
      │                                              │  c. send A2A msg     │
      │                                              │     X-User-Id hdr    │
      │                                              │  d. record dispatch  │
      │                                              │  e. spawn outcome    │
      │                                              │     poller           │
      │                                              └──────────┬───────────┘
      │                                                         │
      │                                                         ▼
      │                                              ┌──────────────────────┐
      │                                              │ Agent Pod (existing) │
      │                                              │  ─ A2A receiver      │
      │                                              │  ─ writes events     │
      │                                              │    back to /api      │
      │                                              │    sessions/{id}/    │
      │                                              │    events            │
      │                                              └──────────┬───────────┘
      │ 4. status update (RunHistoryEntry)                      │
      │      ─ DispatchStatus written immediately               │
      │      ─ Outcome written by poller on terminal state      │
      │                                                         ▼
      └──────────────────────── 3. session events stored under userID=alice@…
```

### 1. CRD (`go/api/v1alpha2/scheduledrun_types.go`)

```go
type ScheduledRunSpec struct {
    Schedule      string         // cron expression (5 fields)
    TimeZone      string         // optional IANA name; default UTC
    AgentRef      AgentReference // existing Agent (cross-namespace allowed by design)
    Prompt        string         // static prompt sent on each run
    Suspend       bool
    MaxRunHistory int32          // default 10, max 100
}

type ScheduledRunStatus struct {
    LastRunTime *metav1.Time
    NextRunTime *metav1.Time          // owned by scheduler, refreshed after each fire
    RunHistory  []RunHistoryEntry
    Conditions  []metav1.Condition    // Accepted (controller-owned)
}

// Two-stage status: dispatch is synchronous, outcome resolves async.
type RunHistoryEntry struct {
    StartTime       metav1.Time
    CompletionTime  *metav1.Time
    DispatchStatus  DispatchStatus    // Dispatched | DispatchFailed (synchronous)
    DispatchMessage string
    SessionID       string
    Outcome         RunOutcome        // Pending | Succeeded | Failed | Timeout (async)
    OutcomeMessage  string
    OutcomeTime     *metav1.Time
}
```

A constant `AnnotationCreatedBy = "kagent.dev/created-by"` carries the user
identity used for session ownership.

#### Two-stage success semantics

The earlier model conflated "the A2A request returned 200" with "the agent
finished successfully." Those are very different signals:

- **DispatchStatus** (synchronous): did the request leave the controller and
  reach the agent's HTTP listener? `Dispatched` means yes; `DispatchFailed`
  means the agent was unreachable, the URL was wrong, the model config was
  missing — anything where we couldn't even hand the work off.
- **Outcome** (asynchronous): did the agent's session actually complete?
  `Pending` while the agent is still working, then resolves to `Succeeded` /
  `Failed` / `Timeout` when the underlying A2A task reaches a terminal state.

A user looking at the run history for "did this scheduled report actually go
out?" cares about `Outcome`. A user debugging "why is nothing happening?"
cares about `DispatchStatus`. The UI surfaces the resolved cell — `Succeeded`
when both stages agree, `Dispatch Failed` short-circuits and skips outcome
polling, `Running` while pending.

### 2. HTTP API (`go/core/internal/httpserver/handlers/scheduledruns.go`)

| Method | Path | Notes |
|--------|------|-------|
| GET    | `/api/scheduledruns` | List all SRs |
| GET    | `/api/scheduledruns/{ns}/{name}` | Read |
| POST   | `/api/scheduledruns` | Create — sets `created-by` annotation from request |
| PUT    | `/api/scheduledruns/{ns}/{name}` | Update spec, preserves `created-by` |
| DELETE | `/api/scheduledruns/{ns}/{name}` | Delete |
| POST   | `/api/scheduledruns/{ns}/{name}/trigger` | Manual run (skips schedule, ignores `suspend`) |

Validation rejects bad cron expressions and bad time-zone names at the
handler layer (`ValidateSchedule`). We do **not** enforce a minimum interval
— operators may need fast cadences for testing, and admin-only exemptions
add complexity without clear payoff. LLM cost / load is the operator's
responsibility.

Manual trigger deliberately ignores `spec.suspend`: suspend stops the cron
engine from firing automatically; a human pressing "Run now" is an explicit
override. The cron schedule remains paused.

Agent deletion is gated by reference protection (see "Agent ↔ ScheduledRun
referential integrity" below).

### 3. Controller (`go/core/internal/controller/scheduledrun_controller.go`)

Watches `ScheduledRun` events and `Agent` events. On each reconcile:

1. Validate `spec.timeZone` (rejects unknown IANA names) — done **before**
   cron parsing, otherwise a bad TZ surfaces as `InvalidSchedule` because
   `cron.ParseStandard` rejects the `CRON_TZ=` prefix.
2. Parse cron, set `Accepted=False` on parse failure with reason
   `InvalidSchedule`.
3. Verify the referenced `Agent` exists. On 404 the controller also calls
   `scheduler.RemoveSchedule(key)` so a tick can't keep firing a Failed run
   forever, and sets `Accepted=False` with reason `AgentNotFound`.
4. Call `scheduler.UpdateSchedule(sr)` which adds/updates the cron entry.
5. On deletion, the watch fires once with `IgnoreNotFound`; we call
   `scheduler.RemoveSchedule(key)`.

The Agent watch uses a **field index** (`spec.agentRef`, keyed by
`namespace/name`) so that an Agent create/update fans out only to SRs that
actually reference it (`O(matched)` instead of `O(all SRs)`). The empty-
namespace case resolves to the SR's own namespace at index time, matching
the controller's resolution rule.

#### Status ownership split

The controller and the scheduler both write to `status`, so we partition by
field to avoid clobbering writes:

| Field | Owner |
|---|---|
| `conditions["Accepted"]` (`InvalidTimeZone`/`InvalidSchedule`/`AgentNotFound`/`ScheduleAccepted`) | Controller |
| `lastRunTime`, `nextRunTime` | Scheduler |
| `runHistory[]` | Scheduler |
| `observedGeneration` | Controller |

`NextRunTime` was previously computed by the controller, but the scheduler
already knows the freshest value (it re-computes after each fire). Moving it
to the scheduler also keeps `NextRunTime` accurate even when the controller
isn't reconciling.

### 4. Scheduler (`go/core/internal/controller/scheduledrun_scheduler.go`)

A `manager.Runnable` that wraps `robfig/cron/v3`. Two design choices worth
noting:

- **`NeedLeaderElection() = true`** — only one controller replica fires the
  schedule, so HA deployments don't double-fire.
- **No persistent run queue** — when the cron tick fires we immediately create
  the session and dispatch A2A in-process. Crash loses the in-flight run; see
  Known Issues.

#### Time-zone handling

`spec.timeZone` is fed to `robfig/cron/v3` via the `CRON_TZ=America/Los_Angeles `
prefix that its standard parser already supports. The helper
`scheduleSpecForCron(sr)` returns the prefixed string when `TimeZone` is set,
otherwise the bare cron expression (interpreted as UTC).

#### Per-tick flow (`runOnce`)

1. Re-fetch the `ScheduledRun` (cron entry holds only the namespaced name, not
   stale spec).
2. Honor `Suspend` — return early without recording a history entry.
3. Read `kagent.dev/created-by` annotation; that's the **session userID**.
   Falls back to literal `"scheduled-run"` if absent (for SRs created via
   `kubectl apply` without going through the API).
4. Create a row in the sessions table.
5. Send the A2A message via `a2aclient.NewA2AClient` with an
   `X-User-Id`-injecting `RoundTripper`, so the agent runtime persists events
   under the same userID.
6. Append a `RunHistoryEntry{DispatchStatus, SessionID, Outcome=Pending}` and
   write status. Trim to `MaxRunHistory` (fallback 10 if unset, since the fake
   client doesn't apply CRD defaults).
7. Refresh `NextRunTime` from the cron entry.
8. If dispatch succeeded, **spawn an outcome poller goroutine** that polls
   `dbClient.ListTasksForSession(sessionID)` until the task hits a terminal
   A2A state (`Completed`/`Canceled`/`Failed`/`Rejected`). It resolves
   `Outcome` on the matching history entry. If the poll exceeds
   `outcomePollTimeout` (15m) it writes `Outcome=Timeout`. Failed dispatches skip
   polling — they have no session to poll.

#### Manual trigger path

`TriggerManualRun(key)` reuses `runOnce` minus the `Suspend` early-return,
returning the resulting `RunHistoryEntry` so the HTTP handler can render a
toast immediately. The outcome poller still runs asynchronously — the user
sees `Dispatched` immediately and can refresh to see the resolved outcome.

### 5. UI

- Header → **View → Scheduled Runs** lists `/schedules`.
- Header → **Create → New Scheduled Run** opens the create form
  (`/schedules/new`). The form accepts an optional time-zone string.
- Detail page (`/schedules/[namespace]/[name]`) shows the run history with
  links into the chat session — the session userID match with the current
  user is what makes those links resolve.
- The run-history table maps `(DispatchStatus, Outcome)` to a single status
  badge: `Dispatch Failed` (red), `Succeeded` (green), `Failed` / `Timeout`
  (red / amber), `Running` (blue, while `Outcome=Pending`), `Dispatched`
  (outline) as fallback.

### 6. Metrics

The scheduler registers three Prometheus metrics with the controller-runtime
metrics registry (`go/core/internal/metrics/scheduledrun.go`):

- `kagent_scheduledrun_active_schedules` — gauge of currently-loaded SRs.
- `kagent_scheduledrun_dispatch_total{namespace,name,status}` — counter of
  dispatch attempts, status `Dispatched`/`DispatchFailed`.
- `kagent_scheduledrun_outcome_total{namespace,name,outcome}` — counter of
  resolved outcomes.
- `kagent_scheduledrun_dispatch_duration_seconds{namespace,name}` — histogram
  of synchronous A2A dispatch latency.

The labels are intentionally namespace+name (low cardinality in practice).
For very large clusters this could be aggregated upstream.

---

## Design Rationale

### Why a CRD, not a Kubernetes `CronJob`?

A native `CronJob` would spin up a pod per tick and send the A2A request from
that pod. We rejected this because:

1. The agent already runs as a long-lived service; we don't need a per-run pod.
2. We want the session to land in kagent's own database with the correct
   userID, which means the trigger has to flow through code that knows about
   kagent's session model.
3. Cron expression validation, reference integrity, run history, and UI surface
   all want a first-class kagent resource, not a bag of opaque `Job` objects.

### Why split DispatchStatus from Outcome?

A single status field forced a bad choice: report success on dispatch (which
lies when the agent then errors out) or block the controller until the agent
finishes (which couples the controller's loop to the agent's wall-clock and
breaks fast cron cadences). The two-stage model lets the controller move on
immediately while a background poller resolves the truth from
`database.Client.ListTasksForSession` — the same source of truth the UI uses
for chat sessions, so the two views can never disagree.

### Why one annotation for `created-by` instead of a `spec.user` field?

Spec fields are user input; annotations are metadata. The user identity is
captured server-side from the HTTP principal — putting it in the spec would
mean:

- Clients could lie about it.
- It would show up in YAML examples and confuse users into editing it.

Annotations also let us evolve later (e.g., switch to a label, add multiple
attribution keys) without a breaking spec change.

### Why allow cross-namespace `agentRef`?

`agentRef.namespace` defaults to the SR's namespace but may be set to any
namespace the controller has permission to read. This is by design: it's
common to keep agents in a shared `kagent` namespace and run schedules from
team namespaces. We do not enforce a same-namespace restriction. The
trade-off is that an SR can reference an agent its creator may not have
direct RBAC on; in practice the controller's service account is the one that
reads the agent, so cluster admins control reachability through standard
RBAC on the `Agent` resource.

### Why no minimum cron interval?

An earlier iteration enforced a 1-hour floor at the API layer to prevent
runaway loops like `* * * * *` from silently burning LLM quota. We dropped
the floor: operators legitimately need fast cadences for testing, and once
you carve out an admin exemption the rule becomes arbitrary. Cost containment
belongs in per-namespace quota tracking, not a hardcoded number in the
validator. Today the validator only checks **that the cron expression and
time zone parse**.

### Why does manual trigger ignore `suspend`?

`suspend=true` stops the scheduler from auto-firing. A human clicking "Run
now" is an explicit override of automation. If we blocked manual triggers
under suspend the user would have to un-suspend, run, re-suspend — which
also opens a window where the schedule auto-fires unintentionally. The
schedule itself remains paused; only the explicit one-shot proceeds.

### Why is the scheduler a `Runnable` rather than its own deployment?

It needs to read `ScheduledRun` CRs from the cache and write status updates,
which means it's already coupled to the controller manager. Splitting it into a
separate deployment would force a kube-API-only path with no shared cache,
adding latency and complexity for no real benefit.

### Agent ↔ ScheduledRun referential integrity (option B)

When deleting an Agent, the HTTP handler lists `ScheduledRun`s and rejects the
delete with **HTTP 409 Conflict** if any reference the agent
(`go/core/internal/httpserver/handlers/agents.go::findReferencingScheduledRuns`).
The error message lists the offending SRs.

We picked **B (block)** rather than A (warn-then-delete) because:

- An orphaned SR would log "Agent not found" forever and clutter the controller.
- The warn-then-delete UX adds a confirm step but doesn't actually prevent the
  bad outcome (tired user clicks confirm anyway).
- Block is reversible — user deletes the SR first, then the Agent.

The UI (`DeleteAgentButton.tsx`) surfaces the 409 message via a sonner toast so
the failure is visible, not silent in console.

If the Agent is force-deleted out from under an SR (e.g., raw `kubectl
delete`), the controller's Agent watch fires and the SR's reconcile flips
`Accepted=False/AgentNotFound` and removes the cron entry — so we self-heal
even when the API guard is bypassed.

### Why ScheduledRun → Session uses an A2A client, not a database insert?

We could write events directly into the sessions table to bypass the agent
pod entirely. That would be cheaper, but it would also bypass the agent's
actual logic (LLM call, tool execution), which is the whole point. Going
through A2A means the run is **identical** to a UI-initiated chat: same
session schema, same event shape, same UI rendering.

---

## Scenarios Covered

The feature is scoped to: **"trigger an existing agent on a cron schedule"**.
Within that scope, the following are verified end-to-end against a kind
cluster:

| Scenario | Verified |
|---|---|
| Create SR via UI / API, see it on the list page | ✅ |
| Update every spec field (schedule, timeZone, agentRef, prompt, suspend, maxRunHistory) | ✅ |
| Delete SR | ✅ |
| Cron tick triggers agent with the right prompt | ✅ |
| Manual trigger (`/trigger` endpoint) ignores `suspend` and dispatches | ✅ |
| Run history records start, dispatch status, session ID, then async outcome | ✅ |
| Failed dispatch (agent unreachable) recorded with `DispatchStatus=DispatchFailed` and skips outcome polling | ✅ |
| Successful dispatch + agent error recorded with `DispatchStatus=Dispatched`, `Outcome=Failed` | ✅ |
| Outcome poller writes `Outcome=Timeout` when agent doesn't reach terminal state in time | ✅ |
| `Suspend=true` skips auto-fire but allows manual trigger | ✅ |
| Time zones: `0 9 * * *` with `Asia/Shanghai` fires at 09:00 Shanghai | ✅ |
| Bad time zone sets `Accepted=False/InvalidTimeZone` | ✅ |
| Bad cron sets `Accepted=False/InvalidSchedule` | ✅ |
| Missing agent sets `Accepted=False/AgentNotFound` and removes the cron entry | ✅ |
| Recreating a missing agent re-arms the schedule (Agent watch fan-out) | ✅ |
| Creator can open the resulting session in the UI chat | ✅ |
| Other users cannot read sessions created by someone else's SR | ✅ |
| Deleting an Agent referenced by an SR is blocked with HTTP 409 + UI toast | ✅ |
| `ScheduledRun.status.runHistory` capped at `maxRunHistory` (default 10) | ✅ |

---

## Known Issues / Limitations

### 1. Annotation-based ownership doesn't handle user deletion

If `alice@example.com` creates an SR and is later removed from the auth system,
the SR keeps firing under her userID and her sessions become orphaned (no
human can read them). There's no "owner exists" check.

**Fix path:** integrate with the auth system's user lifecycle, or add a
soft-deletion / re-attribution flow. Acceptable risk while alpha.

### 2. SR fall-through when controller has no leader

`Runnable.NeedLeaderElection() = true` means the scheduler only runs on the
leader. During leader transition (~15s typical), ticks that fall in the gap
are simply lost — no backfill. There's no automatic catch-up. Acceptable for
hourly+ schedules; for sub-second cadence the gap could miss a tick.

### 3. Crash loses in-flight outcome polls

If the controller restarts while an outcome poller goroutine is mid-flight,
the matching `RunHistoryEntry` stays at `Outcome=Pending` forever. There's
no recovery sweep that resumes polling on startup. **Workaround:** the user
can read the session directly to see the actual result; the SR status just
won't reflect it.

**Fix path:** on startup, scan SRs for `Pending` outcome entries and respawn
pollers for each. Tracked separately from this iteration.

### 4. Interaction with kagent's reconciler is one-way

The `ScheduledRun` controller does NOT trigger any change to the referenced
`Agent` — no reconcile fan-out, no recreate, no restart. This is by design
(SRs are passive consumers), but it means:

- An Agent rolling restart mid-run will fail the in-flight A2A call. Same
  failure mode as a UI-initiated chat against a restarting pod.
- Updating `agentRef` does NOT cancel any in-flight run on the old agent.

We treat both as acceptable; users should drain runs before agent maintenance.

### 5. No first-class concurrency policy

The earlier `ConcurrencyPolicy=Forbid|Allow|Replace` field has been removed.
Agents serialize per-session, so two overlapping SR ticks land in two
sessions and don't interleave. If a future use case (resource budgets,
prompt-level locking) needs explicit concurrency, it should be designed
fresh against the agent/session model rather than reintroducing the
`CronJob`-shaped vocabulary.

---

## Files

| File | Role |
|---|---|
| `go/api/v1alpha2/scheduledrun_types.go` | CRD types + `AnnotationCreatedBy` |
| `go/api/config/crd/bases/kagent.dev_scheduledruns.yaml` | Generated CRD manifest |
| `helm/kagent-crds/templates/kagent.dev_scheduledruns.yaml` | CRD shipped via Helm |
| `helm/kagent/templates/rbac/{getter,writer}-role.yaml` | RBAC additions |
| `go/core/internal/controller/scheduledrun_controller.go` | Kubernetes controller, Agent watch fan-out via field index |
| `go/core/internal/controller/scheduledrun_scheduler.go` | Cron engine, dispatcher, outcome poller |
| `go/core/internal/metrics/scheduledrun.go` | Prometheus metrics |
| `go/core/internal/httpserver/handlers/scheduledruns.go` | REST handlers |
| `go/core/internal/httpserver/handlers/agents.go` | Reference protection on agent delete |
| `go/core/pkg/app/app.go` | Wires controller + scheduler into the manager |
| `ui/src/app/schedules/**` | UI list / detail / create pages |
| `ui/src/components/schedules/**` | List + run-history components |
| `ui/src/components/Header.tsx` | Create + View dropdown entries |
| `ui/src/components/DeleteAgentButton.tsx` | Surfaces 409 toast on delete-blocked |
| `go/core/test/e2e/scheduledrun_api_test.go` | E2E suite |
