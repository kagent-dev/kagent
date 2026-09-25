# Runtime and Lifecycle

An `AgentInstance` is PostgreSQL-backed control-plane state exposed through gRPC.
It pins one prepared revision and names one Substrate Actor. It is not a
Kubernetes resource.

## Creation and state

Creation selects the latest successful revision for the Harness/AgentTemplate
pair, creates a deterministic Actor initially suspended, and marks the instance
ready after Substrate accepts it. Readiness of the image was already established
while preparing the ate-api ActorTemplate; AgentInstance creation does not resume
an Actor merely to probe `/readyz`.

Create (including forks), explicit Suspend, Resume, and Delete keep their current
operation UUID and executor claim on the instance row. Fork creation loads its
pinned checkpoint from PostgreSQL. Namespace provisioning belongs to the template
controller; instance creation uses the pinned ActorTemplate's existing namespace.
Read-only preparation may run concurrently, but an atomic execution claim permits
exactly one caller to issue runtime mutations. Network work holds no database
transaction or lock. Completion changes the instance atomically and retains its
operation UUID until a later transition supersedes it.

A joined caller observes the current instance only while its admitted generation
remains current. A superseded caller gets a conflict and issues no runtime work,
even when the new operation has the same state and kind. There is no historical
lifecycle result archive or pruning requirement. Creation retries return current
instance state; already-at-target Suspend/Resume requests are successful no-ops.
Neither requires an old operation receipt. A2A message deduplication and event
replay have their own durable history requirements and are unchanged.

An unclaimed operation can retry preparation; Delete may supersede it. Preparation
failure invalidates its generation. Once claimed, an operation never expires. A
timeout, disconnected client, process restart, lost runtime response, or runtime
success whose database completion fails leaves the operation pending and blocks
conflicting lifecycle work, including Delete. The instance retains its revision
and checkpoint pins. Only the original executor with a known successful response
may finish persistence; a favorable Actor read alone does not establish that an
earlier request has stopped. There is no automatic takeover or administrative
unlock API for uncertain operations.

Deletion retains an indefinitely kept DELETED instance tombstone with its owner,
creation request identity, and final operation UUID. It clears runtime routing,
releases the revision and checkpoint pins, and revokes shares atomically. A fork's
source checkpoint UUID remains as request identity; a generated foreign-key column
pins that checkpoint only while the instance is live. Ordinary instance/task/share
access excludes deleted instances. Create/Fork request IDs remain reserved after
deletion and cannot recreate compute. Public Delete still returns NotFound for a
fresh request after deletion; already-authorized joined Delete callers can observe
the final tombstone. No public operation API is introduced.

Explicit suspend and resume update the logical lifecycle state. Deletion closes task
admission, stops and deletes the Actor, then tombstones the instance. The workflow
entry points are in
[`go/core/internal/service/agentinstance`](../../go/core/internal/service/agentinstance).
This serialization covers explicit lifecycle calls only: A2A, Pause, Quiesce, and
checkpoint execution still need shared ownership before multi-replica gateway use.

The unreleased schema requires a clean database. Do not overlap older binaries that
can issue lifecycle calls without instance-local execution claims. PostgreSQL tests with controlled
Actor responses verify claim ordering, delayed callers, and lost responses; live
Substrate settlement and the complete multi-replica rollout remain acceptance work.

## Automatic quiescence

After an A2A task reaches a quiescent boundary—terminal, `input-required`, or
`auth-required`—the gateway asks the lifecycle workflow to quiesce the Actor.
Quiescence suspends compute and returns the exact snapshot identity while leaving
the AgentInstance logically ready. Substrate ingress resumes a suspended Actor
automatically when the next interaction arrives.

Runtime calls and quiescence are serialized by an in-memory coordinator so a
late suspend cannot race a new turn in one process. This intentionally limits the
gateway to one replica until coordination is moved to a shared store.

```mermaid
sequenceDiagram
    participant Client
    participant Gateway
    participant DB as PostgreSQL
    participant Workflow as AgentInstance workflow
    participant Actor as Substrate Actor
    Client->>Gateway: send or continue A2A task
    Gateway->>Actor: invoke (ingress resumes if suspended)
    Actor-->>Gateway: quiescent event
    Gateway->>Actor: close runtime stream
    Gateway->>Workflow: quiesce instance
    Workflow->>Actor: suspend
    Actor-->>Workflow: exact snapshot identity
    Workflow-->>Gateway: snapshot boundary
    Gateway->>DB: store task + event + snapshot atomically
    DB-->>Gateway: committed
    Gateway-->>Client: publish quiescent event
    Note over Workflow,Actor: AgentInstance remains logically ready
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

## Runtime revision cleanup metrics

The controller leader collects unreferenced runtime revisions at startup and
every minute. Each candidate has a one-minute deadline; failed deletions remain
eligible for retry without preventing other candidates from being attempted.
Pair, instance, checkpoint, and UID protections still apply.

GC uses the controller's shared OpenTelemetry MeterProvider. The instruments
are defined in the Weaver registry and are available through the existing
Prometheus reader and configured OTLP export. This instrumentation does not
create a provider or enable a listener: scraping remains opt-in through
`controller.metrics.enabled`, with existing authentication settings unchanged.

| OTel metric | Instrument and unit | Meaning |
| --- | --- | --- |
| `kagent.runtime_revision.gc.pending` | Observable integer gauge, `{revision}` | Eligible persisted revisions from the last successful discovery, including incomplete deletions. No application attributes. |
| `kagent.runtime_revision.gc.failures` | Integer counter, `{failure}` | Failed attempts, with only `kagent.gc.stage=discovery\|collection`. No revision, template, UID, namespace, or error attributes. |

Prometheus renders these as `kagent_runtime_revision_gc_pending` and
`kagent_runtime_revision_gc_failures_total`, with the failure attribute rendered
as `kagent_gc_stage` (not the previous `stage` label).

Pending count is sampled at the start and once at the end of each sweep,
including an empty sweep. Scrapes perform no database or network I/O. Discovery
errors retain the previous count and increment `discovery` failures; an initial
error stops the sweep. Parent cancellation is not counted as failure, but an
operation's own deadline while its parent remains active is.

Pending is absent before successful discovery, on standby replicas, and after
GC stops. This replaces the previous Prometheus-only `NaN` representation:
integer gauges cannot represent `NaN`, and a synchronous gauge would retain
its last value after collection stops. The observable callback reads only a
cached count and is unregistered when GC stops. Do not fill missing samples
with zero: only successful empty discovery reports zero.
Restart reconstructs count from PostgreSQL and resets process-local counters.
Use reset-aware `rate` or
`increase`, not raw counter differences. No age or freshness metric is exposed;
counts can remain stale during slow cleanup or after discovery errors.

When the registry is exported, scope queries to the active controller:

- Growing pending count without collection failures suggests churn or slow
  sweeps; rising discovery failures instead warrant database investigation.
- Repeated collection failures with a positive backlog require correlating
  logs by `revision`, `actor_template_atespace`, `actor_template_name`, and
  `error`. Repeated same-object Substrate deletion errors distinguish persistent
  failure from unrelated backlog turnover; claim or finalization errors can
  instead indicate a database problem.

Restore the failing dependency and let GC retry. Successful finalization is
reflected in the next successful discovery; historical counters do not decrease.
Do not bypass reference or UID protections or remove deletion markers to clear
the gauge. Checkpoint recovery is outside this instrumentation's scope.
