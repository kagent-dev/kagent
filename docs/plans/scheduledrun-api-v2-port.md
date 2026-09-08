# ScheduledRun: PostgreSQL API and port of PR #2097

Updated 2026-09-08. ScheduledRun is a creator-owned PostgreSQL/gRPC object.
Each firing has a separate ScheduledRunExecution record and creates a fresh
AgentInstance. Execution identity and history survive conversation deletion.
Harness and AgentTemplate remain Kubernetes configuration resources.

## Implementation progress

The port includes main through Substrate 0.0.26 (#2738). Original contributor
commits `7e6ad0dd` and `f68a7cc0` remain ancestors with their original authors and
SHAs. The separate AgentInstance tombstone PR is not a dependency.

Implemented:

- Schedule CRUD, creator isolation, etag updates, pause, deletion markers,
  cron/timezone validation, and create-request idempotency.
- Manual and due reservations create a dedicated execution row. Due reservation
  and schedule advancement commit together; neither requires a prepared runtime.
- Execution records own firing identity, immutable prompt/deadline, status, and an
  optional historical instance ID. Trigger returns an execution. Dedicated get/list
  RPCs provide creator-scoped, paginated execution history, newest first, including
  after deletion of the schedule or instance.
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
4. Kind E2E: passed against fresh PostgreSQL on Substrate 0.0.25, then repeated on
   0.0.26 after merging main. Cron/manual firing, timeout cleanup, and controller
   restart recovery run through real gRPC, A2A, and Substrate boundaries with a
   controlled model. The live browser create/reload/edit/delete test also passed
   against 0.0.26.

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
The final full mock browser run finished with 202 passes and one Firefox chat
startup failure. Its trace recorded `NS_ERROR_FILE_NO_DEVICE_SPACE` while loading
an application module; `/tmp` was 94% full. The affected test passed on rerun with
browser profiles on the workspace filesystem. All scheduling tests passed in both
Chromium and Firefox. The schedule creation test waits for the dialog animation
before submitting the empty form. Source lint has seven existing warnings; plain
`yarn lint` also scans unrelated old build artifacts in this workspace.
The live Chromium test includes `America/New_York` and a fractional timeout. It
exposed missing timezone data in the controller image; embedding `time/tzdata`
fixed the real API rejection. The final review also corrected history ordering:
new executions appear on page one instead of behind older pages; real database and
gRPC pagination tests verify the descending cursor.
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

## Attribution and merge policy

This port follows the original PR #2097 snapshot at `f68a7cc0`. Its core feature
commit is `7e6ad0dd` by 0xLeo258. The original CRD reused an agent runtime and
created a fresh session per firing; this port preserves independent conversations,
manual triggers while paused, overlapping firings, and no catch-up backlog.

Commit `7d13eb60` implements the port on the current API. Merge `0513fec0` retains
the original branch using the `ours` strategy because the legacy CRD, REST, and
Session implementation has been replaced completely. That merge changes no files.
Both original contributor commits are verified ancestors. The original worktree
remains untouched as a backup.

PR #2097 has since been rewritten against `release/v0.10.x` (head `56d7330d` when
checked on 2026-09-08). Publish this as a successor against main, linking #2097 and
crediting 0xLeo258; do not overwrite their release branch.

The repository currently allows only squash merges. The PR branch retains the
original history, but preserving those commits on main requires enabling and using
a merge commit. If the repository keeps its squash-only policy, include
`Co-authored-by: 0xLeo258 <noixe0312@gmail.com>` in the squash commit to retain credit;
the original SHAs will remain on the PR branch rather than in main's ancestry.
