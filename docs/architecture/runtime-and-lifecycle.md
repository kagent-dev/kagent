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
