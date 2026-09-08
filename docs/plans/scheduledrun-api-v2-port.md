# ScheduledRun: PostgreSQL API and port of PR #2097

Updated 2026-09-08. ScheduledRun is a creator-owned PostgreSQL/gRPC object.
Each firing has a separate ScheduledRunExecution record and creates a fresh
AgentInstance. Execution identity and history survive conversation deletion.
Harness and AgentTemplate remain Kubernetes configuration resources.

## Implementation progress

Active workspace: repository CWD, branch `feat/scheduled-run-current-api`, based
on merged namespace-cleanup PR #2730 (`c677a207`). Changes are uncommitted.
The original contributor branch and its resolved, uncommitted merge remain saved
in `/tmp/kagent-scheduled-run-v2`; scheduling also has a pre-port stash backup.
Contributor ancestry must be incorporated before publishing the scheduling PR.
The separate AgentInstance tombstone PR is not a dependency.

Implemented:

- Schedule CRUD, creator isolation, etag updates, pause, deletion markers,
  cron/timezone validation, and create-request idempotency.
- Manual and due reservations create a dedicated execution row. Due reservation
  and schedule advancement commit together; neither requires a prepared runtime.
- Execution records own firing identity, immutable prompt/deadline, status, and an
  optional historical instance ID. Trigger returns an execution. Dedicated get/list
  RPCs provide creator-scoped, paginated execution history, including after deletion
  of the schedule or instance.
- A separate store operation atomically creates an ordinary instance/A2A context
  and links it to the execution. It pins the ready revision at instance reservation.
  Concurrent retries return the same link. A link to a deleted instance never
  creates a replacement; an expired unstarted execution becomes TIMED_OUT.
- No scheduling fields, schedule-specific reads, or tombstone exception remain on
  AgentInstance. Existing instance deletion and A2A retention behavior are unchanged.
- Core baseline migration `000001_initial.sql`, generated SQL/protobuf contracts,
  and focused unit, real PostgreSQL, and generated-client gRPC coverage.

The service and scheduling worker are wired into `app.Run`. The manager runs the
worker under leader election. It reserves due executions, provisions their linked
instances, sends the saved prompt through the private A2A runtime boundary, and
persists the original task reference, outcome, completion time, and failure reason.
Trigger still returns acceptance immediately. The UI and live Substrate scheduling
tests are implemented; actor-to-tool identity and user-grant elicitation are deferred.

## Model and public API

| Object | Owns |
| --- | --- |
| Harness + AgentTemplate | Reusable runtime and agent configuration |
| ScheduledRun | Creator, immutable target pair, prompt, cron/timezone, pause state, timeout |
| ScheduledRunExecution | One firing's identity, immutable inputs, scheduling status, and historical instance link |
| AgentInstance | Conversation, pinned revision, runtime lifecycle, and sharing |
| A2A Task | Work state, messages, artifacts, streaming, and outcome |

The original CRD reused an agent runtime but created a fresh session per firing.
The new API identifies a conversation with its instance, so a fresh instance per
execution preserves that independent-conversation behavior. Reusing an instance
is outside the initial scope; the separate execution identity leaves that possible.

ScheduledRunService exposes schedule create/get/list/update/delete/trigger and
execution get/list. Updates replace mutable configuration with a required etag.
Trigger requires a request ID and returns the accepted execution immediately.
The historical `agent_instance_id` is empty until instance reservation; after that
it stays stable even when the instance no longer exists. A2A remains authoritative
for task details and transcripts. Execution state summarizes the initial scheduled
invocation, not later user continuation or the instance's runtime lifecycle.

The server assigns creator, IDs, timestamps, etag, next due time, and execution
inputs. Database objects have no namespace. Target references are typed; the
Harness and AgentTemplate must share their Kubernetes namespace. Protobuf validation owns
request-intrinsic rules; the service owns authorization and target admission.

## Ownership and execution authority

The human creator owns the schedule and its conversations; creator is not an
instruction to execute with that user's credentials. The controller creates and
invokes the runtime under its control-plane authority. Substrate Actor identity
is the eventual machine identity for runtime/tool access, reached through the
existing execution -> AgentInstance -> Actor relationship. Do not add a separate
machine identity registry, execution identity column, or background user session.

Wiring Substrate actor credentials into tool calls and user authorization
elicitation are explicitly deferred. The initial worker uses the configured
runtime/tool authentication mechanisms. Keep credential selection and forwarding
behind the existing AuthProvider/UpstreamAuth boundary and permission checks in
Authorizer; the scheduler supplies immutable execution inputs and invokes the
runtime workflow. Actor verification and tool-specific credential integration can
be added at their owning boundaries without changing schedule persistence.

Worker calls carry `auth.ControlPlaneSession`, which contains no human identity,
claims, or credentials and does not assert an Actor identity. Custom authorizers
can identify that session in the context. They must allow `create` on
`ScheduledRunExecution` (`execution_id`) and the existing target-pair
permissions: Harness/AgentTemplate reads and AgentInstance creation for the target.
The configured `AuthProvider.UpstreamAuth` receives this session and the target
instance, so deployments can supply control-plane credentials or reject the call.
Default providers forward no human headers for this session. Public ownership and
target-admission checks remain in place. Internal task operations are not public
RPC handlers.

Later, a tool may request a scoped user grant and resume the same A2A task after
authorization. That grant does not replace the actor's execution identity. Neither
user tokens nor a snapshot of user claims belongs in schedule/execution records.

Defaults and behavior:

- Five-field cron, IANA timezone (default UTC), nonblank prompt up to 32 KiB,
  default timeout 15 minutes. Use robfig/cron/v3 and time.LoadLocation, with Go's
  embedded timezone database so minimal controller images support IANA zones.
- Pausing prevents automatic firings but permits explicit manual triggers.
- Edits affect only subsequently reserved executions. Target and ownership stay
  immutable; accepted prompt, trigger identity, and deadline never change.
- No catch-up backlog: skip unreserved firings more than 30 seconds late and
  compute the next future occurrence. Accepted executions must recover on restart.
- Firings may overlap because their conversations are independent.
- Deleting a schedule prevents new firings without cancelling accepted executions.
  Trigger retries return the original execution, including after instance deletion.
- Instance shares grant no schedule or execution-history access.

## Persistence and atomic boundaries

The unreleased core baseline `000001_initial.sql` includes `scheduled_run` and
`scheduled_run_execution`. The optional vector store retains its independent baseline.

Both tables store serialized protobufs in `data BYTEA`. SQL retains only fields
used for ownership, queries, uniqueness, constraints, and worker coordination:

- Schedule: ID, creator, create-request ID/hash, next due time, deletion marker.
  Config, target references, etag, and creation/update timestamps live in the payload.
- Execution: ID, schedule ID, scheduled time/manual request ID, historical instance
  ID, task ID, state, completion time, next attempt, and lease token. Immutable
  prompt, creation time, deadline, creator, and failure reason live in the payload.

Reads overlay authoritative SQL fields onto decoded protobufs. Malformed payloads
return errors. The schedule parent remains after deletion, retaining ownership and
target references. Constraints enforce exactly one trigger, uniqueness per
schedule/time or schedule/manual-request pair, valid states and completion/linkage
invariants. The instance ID deliberately has no foreign key, so history survives
hard deletion. Owner, due-work, execution-history, and worker-queue indexes serve
the queries without inspecting protobuf bytes.

Scheduling builds on the merged protobuf persistence change (#2735). Database
types live in `core/internal/database`, with generated rows private under
`internal/dbgen`; no shared database client interface is reintroduced.
The service and worker each declare the store methods they consume. SQL row locks,
serialization, and query sequencing remain inside the store. A leased item holds
an execution snapshot and a separate `{ExecutionID, Token}` lease. Workers submit
only explicit state/task/failure progress; they cannot overwrite saved inputs,
instance linkage, or lease identity by mutating the snapshot. The store locks and
checks the lease, updates the persisted payload, and rechecks expiry in the
progress UPDATE. A previously recorded task ID cannot be replaced.

One transaction uses SELECT FOR UPDATE on the schedule, reserves executions, and
advances due time. Manual trigger, edit, pause, and delete serialize on that row.
Replays check the original execution before evaluating deletion/configuration.
No runtime readiness or network call is needed to accept a firing.

Instance reservation uses SELECT FOR UPDATE on the execution and commits ordinary
instance/context creation and the historical link together. An unready target
leaves the accepted execution pending with no partial instance. Retry can select a
ready revision later. Once linked, a deleted instance cannot be silently recreated.
An expired, unlinked execution is marked TIMED_OUT without creating a conversation.
No transaction or row lock spans runtime provisioning or A2A dispatch.

## Execution worker

The manager-owned worker polls once per second and handles at most eight executions
concurrently. Each reconciliation has a ten-second network budget and five seconds
for saving its result. Provisioning and dispatch also respect the execution's
original deadline. Slow batches delay the next cron poll; a continuous work queue
is the next step if throughput requires it.

PostgreSQL leases use `FOR UPDATE SKIP LOCKED`. Leasing stores a random UUID token
and reserves the execution for 30 seconds. Progress updates require the same token
and an unexpired lease, so an old controller cannot overwrite a replacement's
result. Updates release the lease and schedule another attempt after one second.
Terminal executions leave the work queue. Leases fence execution-row updates;
they do not cancel an old network request or provide exactly-once tool effects.
The gateway's existing process-local coordination limitation remains unchanged.

MCP, public A2A, and the scheduler consume the same `a2asrv.RequestHandler`,
including its protocol interceptor. The scheduler calls standard `SendMessage`,
`ListTasks`, `GetTask`, and `CancelTask`; no dispatcher or public reconciliation
helpers exist. The worker owns permission checks, deadlines, leases, stable message
IDs, and execution status. The gateway owns task acceptance, runtime observations,
history persistence, and cancellation cleanup for every caller.

The worker authorizes the execution and target without impersonating its owner,
reserves one instance, and converges `ActorWorkflow.Create`. Its control-plane
session passes through gateway authorization before an ownership-independent
instance lookup; human and shared requests retain their existing ownership scope.
The deterministic initial message ID (`scheduled-run/<execution_id>`) identifies
one dispatch. A2A assigns the task ID, which is saved on the execution. Transactional
initial-message uniqueness ensures only the caller accepting that message may send
it to the runtime.

After a restart or a lost response, persisted task history recovers a missing task
link through `ListTasks`. Once linked, only that task ID can complete the execution.
`GetTask` refreshes active tasks without a live stream ingester; quiescent tasks and
suspended instances remain readable from storage. Transport errors leave the last
durable state intact. The worker never resends an uncertain dispatch, even when the
runtime reports TaskNotFound. A crash between storing the task and sending the
request can therefore leave an execution that never ran and eventually times out.
This avoids repeating tool effects without runtime-wide durable idempotency.
A new manual firing is explicit work, with a new execution and conversation.

Terminal and input/auth-required results quiesce the actor and store its snapshot
before publishing the A2A state. Input/auth-required tasks remain RUNNING in the
execution summary until completion or deadline; grant elicitation is not added
here. The original task is the only task that can complete a firing. A later task
in the same conversation cannot change a completed execution's outcome.

Deadline cleanup runs even after dispatch authority is revoked. A partially
provisioned instance is removed through the existing lifecycle workflow; a running
task is canceled through standard A2A. Cancellation stops any local observer,
quiesces compute, and commits the outcome and snapshot together, falling back to
quiescence if runtime cancellation is unavailable. Failed cleanup stays retryable instead of
publishing a terminal execution and abandoning compute. Already quiescent tasks
are retained, including auth-required tasks. Deleted instances fail their firing
without replacement. Execution history and historical references remain available.

## Delivery and validation

1. Contracts/store/service: implemented with independent execution identity/history.
2. Execution: implemented with controller authorization, worker/application wiring,
   provisioning/A2A dispatch, recovery, outcome recording, deadlines, and cleanup.
3. UI: implemented with React Router/antd/SWR schedule CRUD, pause/trigger,
   paginated execution history, and links to existing conversations. Fixtures remain
   opt-in. Edits use etags; create/trigger retries retain their request IDs.
4. Clean-install Kind E2E: passed against this branch's Substrate 0.0.25 and fresh
   PostgreSQL. Cron/manual firing, timeout cleanup, and controller restart recovery
   run through real gRPC, A2A, and Substrate boundaries with a controlled model.

Verify PostgreSQL concurrency/idempotency and migration Up/Down, creator-isolated
execution history, replay after edits and schedule/instance deletion, optional
instance linkage, unready/expired reservations, constraints, protobuf/sqlc
generation, and relevant Go lint. The generated-client integration test uses real
PostgreSQL and a fake gRPC A2A runtime, covering lost responses, worker replacement,
timeout cleanup retries, auth-required tasks, schedule deletion, and controller/
target authorization denial. Separate tests cover lease takeover and stale writes.
Live tests additionally verify distinct instances per firing, manual-trigger
idempotency while paused, original task identity after conversation continuation,
history after schedule/instance deletion, cancellation at deadline, and actor
quiescence. Restart recovery preserves the original instance/task and makes exactly
one model call.

Validated on the namespace-free base: full database, gRPC server, A2A gateway,
scheduling semantics/service, and migration package tests against PostgreSQL;
all Go packages compile; full Go lint, Buf lint/regeneration, and sqlc generation
pass; UI TypeScript checks pass. Regression coverage includes malformed protobuf payloads,
immutable execution inputs despite snapshot mutation, lease takeover/reuse, and
immutable task identity. UI unit tests, typecheck, build, and source lint passed.
The scheduling mock browser tests passed in Chromium and Firefox. The full mock
browser run initially had five failures; corrected navigation expectations and
the affected tests passed with two workers. Source lint has seven existing warnings;
plain `yarn lint` also scans unrelated old build artifacts in this workspace.
The live Chromium test passed create/reload/edit/delete against PostgreSQL,
including `America/New_York` and a fractional timeout. It exposed missing timezone
data in the controller image; embedding `time/tzdata` fixed the real API rejection.
The cluster also logged the existing runtime-revision GC foreign-key error seen
outside scheduling; it did not block these tests and remains outside this change.

Run scheduling E2Es against an isolated, installed cluster with a matching runtime
Harness and snapshot storage. `KAGENT_LOCAL_HOST` must be reachable from workers;
the controller endpoint must reconnect after the optional restart test:

```sh
cd go
KUBECONFIG=/path/to/kubeconfig \
KAGENT_E2E_API_URL=http://127.0.0.1:28083 \
KAGENT_API_URL=http://127.0.0.1:28083 \
KAGENT_LOCAL_HOST=172.18.0.1 \
KAGENT_E2E_RESTART_CONTROLLER=true \
go test -v ./core/test/e2e -run '^TestScheduledRun' -count=1 -timeout=15m
```

The restart test deletes the sole controller pod and is opt-in. Run it on an
isolated cluster. The live browser test requires the ready `kagent/smoke` template
on the `kagent` harness. It keeps its schedule paused while checking persistence:

```sh
cd ui
UI_LOOP_LIVE=true UI_LOOP_LIVE_PORT=8431 \
KAGENT_DEV_CONTROLLER_URL=http://127.0.0.1:28083 \
yarn test:pw playwright/live/schedules.spec.ts --workers=1
```


Keep the original contributor's commits as ancestors. Replace CRD/controller/REST/
legacy Session wiring with current API boundaries. No schedule CRD, Kubernetes
CronJob, generic job framework, or catch-up policy matrix is needed.

## What is actually in the PR

The PR currently targets `release/v0.10.x`. Its GitHub commit/file listing includes
unrelated mainline work. Relative to current main, only five commits are unique:

| Commit | Author | Content |
| --- | --- | --- |
| `7e6ad0dd` | `0xLeo258 <noixe0312@gmail.com>` | Entire ScheduledRun implementation; signed off by the author |
| `f13c8deb` | Eitan Yarmush | Merge main |
| `41d41380` | Eitan Yarmush | Merge main |
| `a1d0bf1d` | 0xLeo258 | Merge upstream/main |
| `f68a7cc0` | 0xLeo258 | Remove one blank line from a handler test |

The feature comparison is **58 files, 8,274 additions, 93 deletions**. The initial
feature commit already contains essentially all of the implementation. Its
scheduler, target resolver, controller, and CRD source are unchanged at PR head.

`git merge-tree --write-tree origin/main origin/pr-2097` reports **32 conflicts**.
Several are modified files that main deliberately deleted. Git also incorrectly
suggests moving the new REST handlers into `internal/service/model`; reject that
directory-rename guess. An automatically merging file is not necessarily reusable:
the old scheduler and Next.js pages are new files and still need semantic changes.

The PR body is stale. Source and tests say:

- Suspension stops cron ticks; **manual triggering remains allowed**.
- Each execution gets an independent session; overlapping executions are allowed.
- Missed ticks are not replayed.
- Status names are `DispatchFailed/InProgress/Succeeded/Failed/TimedOut`.
- A durable database execution table supplements recent executions in CRD status.
- The inspected scheduler/controller contain no dedicated metrics implementation.

Use these implemented semantics as the baseline, rather than the PR description.

## Git procedure and attribution

Recommended procedure, to execute when implementing the port:

```sh
git fetch origin main refs/pull/2097/head:refs/remotes/origin/pr-2097
git worktree add -b feat/scheduled-run-v2 /tmp/kagent-scheduled-run-v2 origin/pr-2097
git -C /tmp/kagent-scheduled-run-v2 merge --no-commit --no-ff origin/main
```

The last command is expected to stop with conflicts. Resolve legacy infrastructure
and deleted files to current main, migrate the retained feature sources, remove
obsolete newly added files, and regenerate outputs. Inspect the complete diff
against main, not just Git's conflict list. Keep the original scheduling semantics and useful
pure helpers/tests. Remove the
ScheduledRun CRD, its generated artifacts, controller, Kubernetes RBAC additions,
and old REST/UI integrations from the resulting tree. The merge commit should
explicitly describe the move to a PostgreSQL-backed API object.

Once validation passes, create a signed-off merge commit describing the port.
Further fixes can be ordinary signed-off commits. This puts both original feature
commits, with their author metadata and original SHAs, in the branch ancestry.
No existing contributor branch needs a force-push.

Before publishing, verify ancestry with `git merge-base --is-ancestor` for
`7e6ad0dd` and `f68a7cc0`, and inspect `git diff origin/main...HEAD`. Retarget #2097
to main if continuing that PR is possible; otherwise open a successor against main
crediting and linking #2097. Preserve commits when merging the finished PR:
**squash-merging would discard this exact history**.

If project policy requires linear history instead, cherry-pick `7e6ad0dd` with
`-x -s` onto current main and resolve it as the port. Git preserves the original
author while changing the commit SHA. The whitespace-only follow-up becomes
irrelevant when the old REST test is removed; record that in the PR description.
This is a valid attribution-preserving alternative, but not exact history retention.

The attribution investigation above predates implementation. Current port and
worker changes are uncommitted in CWD; the original worktree remains a history
backup. No scheduling branch push or PR edit has been made.
