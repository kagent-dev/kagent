# Runtime and Lifecycle

An `Session` is PostgreSQL-backed control-plane state exposed through gRPC.
It pins one prepared revision and names one Substrate Actor. It is not a
Kubernetes resource.

## Creation and state

Creation selects the latest successful revision for the Agent, creates a deterministic Actor initially suspended, and marks the session
ready after Substrate accepts it. Readiness of the image was already established
while preparing the ate-api ActorTemplate; Session creation does not resume
an Actor merely to probe `/readyz`.

Substrate v0.4.0-alpha1 requires protocol-specific egress policies. Kagent allows
each configured HTTP(S) origin, preserving its scheme, DNS name, and port, and
replaces credential headers in that destination's deciding rule. Conflicting
protocols on the same host and port are rejected before Actor creation. Literal
IP allowlists are unsupported by this Substrate release; model, MCP, and telemetry endpoints
must use DNS names. Host-based test services use Kubernetes Services and
EndpointSlices to provide those names.

Create (including forks), explicit Suspend, Resume, and Delete keep their current
operation UUID and executor claim on the session row. Fork creation loads its
pinned checkpoint from PostgreSQL. Namespace provisioning belongs to the Agent
controller; session creation uses the pinned ActorTemplate's existing namespace.
Read-only preparation may run concurrently, but an atomic execution claim permits
one bounded inline attempt to issue runtime mutations. Network work holds no database
transaction or lock. Completion changes the session atomically and retains its
operation UUID until a later transition supersedes it.

A joined caller observes the current session only while its admitted generation
remains current. A superseded caller gets a conflict and issues no runtime work,
even when the new operation has the same state and kind. There is no historical
lifecycle result archive or pruning requirement. Creation retries return current
session state; already-at-target Suspend/Resume requests are successful no-ops.
Neither requires an old operation receipt. A2A task storage and checkpoint
reconstruction have their own durable history requirements.

An unclaimed operation can retry preparation; Delete may supersede it. Every
admitted Session deletion enters DELETING. Preparation failure invalidates its
generation but leaves DELETING in place, keeping task admission closed until a
delete retry completes. Explicit deletion requires a client retry; idle deletion
is retried by the expiration worker. After an attempt issues runtime work, errors
retain the operation and resource pins. The attempt releases its execution claim
on return. A crashed executor's claim expires after at most two minutes, allowing
a client retry to claim the same operation with a new executor ID. Each attempt
uses a context bounded by that interval; no claim renewer or recovery sweep runs.
A stale executor cannot complete or release a newer attempt's claim.

Lifecycle retries inspect the pinned Actor identity and continue Substrate's
reentrant workflows, including completing egress setup for an existing creation.
Already completed creation steps are not repeated. Fork retries must still match
the retained source snapshot. Conflicting Session lifecycle, task, and checkpoint
work remains blocked until the pending operation completes. Releasing an attempt
never clears its durable intent or makes uncertain work an unissued preparation.

Clients must retry the mutation after transient errors; Get only observes it.
If the client stops retrying, the explicit operation remains pending across API
restarts. There is no general automatic lifecycle recovery. The
[client retry contract](../lifecycle-retries.md) covers error codes, deadlines,
creation request IDs, and the limitations of retries and concurrent callers.

Explicit deletion retains an indefinitely kept DELETED session tombstone with its owner,
creation request identity, and final operation UUID. It clears runtime routing,
releases the revision and checkpoint pins, and revokes shares atomically. A fork's
source checkpoint UUID remains as request identity; a generated foreign-key column
pins that checkpoint only while the session is live. Ordinary session/task/share
access excludes deleted sessions. Create/Fork request IDs remain reserved after explicit
deletion and cannot recreate compute. Public Delete still returns NotFound for a
fresh request after deletion; already-authorized joined Delete callers can observe
the final tombstone. No public operation API is introduced.

Explicit suspend and resume update the logical lifecycle state. Deletion closes task
admission, stops and deletes the Actor, then tombstones the session. The workflow
entry points are in
[`go/core/internal/service/session`](../../go/core/internal/service/session).
TaskStore writes and automatic idle lifecycle work use durable task boundaries
alongside these explicit lifecycle claims. Checkpoint reservations also block
conflicting task writes and idle lifecycle work.

The unreleased schema requires a clean database. Do not overlap older binaries that
can issue lifecycle calls without session-local execution claims. PostgreSQL tests with controlled
Actor responses verify claim ordering, delayed callers, and lost responses; live
Substrate settlement and the complete multi-replica rollout remain acceptance work.

## Idle expiration

Sessions expire after seven days without task activity by default, using the same
bounded deletion workflow as Sandboxes. `controller.sessionIdleTTL` in Helm sets
`KAGENT_SESSION_IDLE_TTL` on the controller. Values use Go durations (`168h` for seven days,
`720h` for thirty); `0` disables the worker, including retries, and negative values
are rejected. There is no per-agent override or maximum TTL.

Idle time is measured from the later of session creation and the latest stored
A2A event's database timestamp. Forks retain original event timestamps, so their
own creation time gives them a full idle lifetime. Reads, renames, lifecycle calls,
and retried writes or settlement do not extend it. Running tasks and
INPUT_REQUIRED/AUTH_REQUIRED turns never expire, even after that duration. Pending
lifecycle operations, dispatch reservations, native cleanup, claimed pause/suspend
work, and checkpoint creation also block expiration.

A leader-only worker scans once per minute in bounded pages. Under the same
PostgreSQL row lock used by task admission, it rechecks the idle clock and active
work, then admits a normal DELETE operation with the durable reason `idle_timeout`
and moves the session to DELETING. A turn admitted after the scan but before the
lock prevents expiration. Once expiration is admitted, new task execution is
fenced, including after preparation failures.
The worker uses the existing Actor deletion workflow and execution claims, so
runtime I/O holds no database locks and overlapping attempts cannot issue the
same generation concurrently. Failed expiration resumes on later sweeps or after
leader replacement; changing a nonzero TTL does not cancel admitted deletion.

After runtime deletion succeeds, the normal delete completion transaction removes
the session, shares, runtime row, and creation receipt. GetSession returns NotFound;
CreateSession or ForkSession with the same caller/request ID can create a fresh
session. Explicit client deletion still retains its tombstone and request ID.

The retention rule preserves the existing audit policy: A2A context, tasks, event
history, and explicit checkpoints remain in PostgreSQL. Retained checkpoints can
still be forked after the source session expires and retain their own snapshot
pins. The ordinary Actor deletion workflow removes the session's runtime and
releases its revision/checkpoint references; backend snapshot garbage collection
remains governed by Substrate and retained checkpoint pins. Idle expiration does
not bound audit-history or explicit-checkpoint storage; those need a separate
retention policy. History is no longer accessible through the expired Session API.

`kagent.session.expired` counts completed sweep removals (Prometheus exports
`kagent_session_expired_total`). Each removal logs `expired idle session` at debug
level with `session_id` and `idle_time`.

## Automatic quiescence

The runtime stages a final task update and acknowledges it after native cleanup.
That acknowledgement publishes task state and history atomically, without waiting
for pause/suspend. A Session lifecycle worker independently claims the idle
boundary in PostgreSQL. INPUT_REQUIRED/AUTH_REQUIRED pauses the actor on its node;
terminal work suspends it and records the exact external snapshot. Waiting tasks
are not forkable. The Session stays logically READY, and Substrate ingress
resumes it when another authorized interaction arrives.

Unfinished native cleanup blocks new task writes and explicit lifecycle changes.
After publication, a new turn may supersede idle work before it is claimed. Once
claimed, idle work blocks new execution, explicit lifecycle changes, and checkpoint
capture until its outcome is recorded. The worker performs runtime I/O outside the
database transaction. Successful snapshot references are retried on database failure
without repeating the Substrate operation. Checkpoint creation requires the matching
snapshot and can return FailedPrecondition after task completion while it is pending.

Unclaimed idle work survives API restarts. A claim for possibly issued runtime work
never expires: losing the worker does not prove that the suspend stopped. Uncertain
claims still block new work, but completed results remain readable. The recorded
actor UID is checked before lifecycle calls; a same-name replacement cannot be
adopted implicitly.

```mermaid
sequenceDiagram
    participant Client
    participant Gateway
    participant Actor as Agent runtime
    participant API as TaskStore API
    participant DB as PostgreSQL
    participant Worker as Session lifecycle worker
    Client->>Gateway: authorized send / continuation
    Gateway->>Actor: invoke
    Actor->>API: create and versioned updates
    API->>DB: stage final boundary
    API-->>Actor: committed version
    Actor->>API: settle after native cleanup
    API->>DB: publish task/history atomically
    Actor-->>Gateway: final event
    Gateway->>Actor: close observer connection
    Gateway->>DB: observe publication
    Gateway-->>Client: current public task
    Note over Worker,DB: Idle lifecycle runs independently of the client response
    Worker->>DB: claim idle boundary unless new execution superseded it
    Worker->>Actor: pause or suspend
    Actor-->>Worker: settled native boundary
    Worker->>DB: record snapshot and finish idle claim
```

## Runtime boundaries

- Port `8083` serves native gRPC, gRPC-Web, A2A, authenticated MCP, and health.
- Actor A2A gRPC is private on port `80`.
- Runtime readiness is private HTTP `/readyz` on port `8081`.
- ate-api defaults to `dns:///api.ate-system.svc:443`.

Clients never receive Actor addresses. The gateway derives and dials them through
the private atenetwork router.

Every Actor mounts a Substrate `DurableDir` at `/data`. Harnesses keep private
state there—local framework state, workspaces, and downloaded assets that must
survive Actor replacement. This state is runtime-private; public task history
remains in PostgreSQL.

Templates capture Full snapshots when paused and Data snapshots when suspended.
Substrate v0.4.0-alpha1 resumes a Data snapshot by starting fresh containers from
the OCI image with the saved durable directories. Data restores no longer combine
Golden memory with the Actor's saved data.

The Go ADK opens and migrates its SQLite session store before readiness, but
retains no idle database connections. Full and golden restores preserve guest
memory while rematerializing `/data`, so a connection opened before the snapshot
can retain a stale file identity and reject writes with `SQLITE_READONLY_DBMOVED`.
Closing connections when returned to the pool keeps quiescent snapshots free of
database handles; each later operation opens the current backing file.

## Runtime revision cleanup metrics

GC uses the controller's shared OpenTelemetry provider and configured OTLP export.
Prometheus scraping is opt-in through `controller.metrics.enabled`.

| OTel metric | Instrument / unit | Prometheus name | Meaning |
| --- | --- | --- | --- |
| `kagent.runtime_revision.gc.pending` | Observable integer gauge / `{revision}` | `kagent_runtime_revision_gc_pending` | Eligible persisted revisions from the last successful discovery. No application attributes. |
| `kagent.runtime_revision.gc.duration` | Histogram / `s` | `kagent_runtime_revision_gc_duration_seconds` | Each discovery or collection attempt, including claim, Substrate read/delete, and finalization. `kagent.gc.stage=discovery\|collection`; `error.type` only on failure: a Substrate gRPC code name or `_OTHER`. Parent cancellation is excluded; operation deadlines count as failures. |

Pending is absent before successful discovery, on standby replicas, and after GC
stops. Do not fill absence with zero: zero means a successful empty discovery.
Discovery errors retain the last count. Scrapes only read the cache; restart
reconstructs pending from PostgreSQL and resets process-local histogram totals.

- **Growing pending:** compare attempt rates, failure ratios, and latency on the
  active controller before diagnosing churn versus slow or failing cleanup.
  Let GC retry; never bypass reference/UID protections or clear deletion markers.
- **Rising failure ratio or latency:** use reset-aware `rate` on histogram
  `_count` (failed attempts have `error_type`), grouped by `kagent_gc_stage`,
  and `_bucket` quantiles. Discovery errors point to the database; collection
  errors require checking the bounded error type and logs (`revision`,
  `actor_template_atespace`, `actor_template_name`, `error`) to identify the
  failing dependency and repeated same-object failures.
