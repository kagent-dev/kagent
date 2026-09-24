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
TaskStore admission and finalization use durable task boundaries alongside these
explicit lifecycle claims. Checkpoint reservations also block conflicting admission.

The unreleased schema requires a clean database. Do not overlap older binaries that
can issue lifecycle calls without instance-local execution claims. PostgreSQL tests with controlled
Actor responses verify claim ordering, delayed callers, and lost responses; live
Substrate settlement and the complete multi-replica rollout remain acceptance work.

## Automatic quiescence

The runtime stages a final task update and acknowledges it after native cleanup.
An API finalization worker claims that boundary in PostgreSQL, outside any client
observation lifetime. INPUT_REQUIRED/AUTH_REQUIRED pauses the actor on its node;
terminal work suspends it and records the exact external snapshot. Waiting tasks
are not forkable. The AgentInstance stays logically READY, and Substrate ingress
resumes it when another authorized interaction arrives.

The unpublished boundary blocks new admission and explicit lifecycle changes.
The worker performs runtime I/O outside the database transaction and then publishes
task state, archived history, and snapshot atomically. Unissued boundaries survive
API restarts. A claim for possibly issued runtime work never expires: losing the
worker does not prove that the suspend stopped. The recorded actor UID is checked
before lifecycle calls; a same-name replacement cannot be adopted implicitly.

```mermaid
sequenceDiagram
    participant Client
    participant Gateway
    participant Actor as Agent runtime
    participant API as TaskStore API
    participant DB as PostgreSQL
    participant Worker as Finalization worker
    Client->>Gateway: authorized send / continuation
    Gateway->>Actor: invoke
    Actor->>API: admit and versioned saves
    API->>DB: stage final boundary
    API-->>Actor: committed version
    Actor->>API: settle after native cleanup
    API->>DB: acknowledge version
    Actor-->>Gateway: final event
    Gateway->>Actor: close observer connection
    Worker->>DB: claim settled boundary
    Worker->>Actor: pause or suspend
    Actor-->>Worker: settled native boundary
    Worker->>DB: publish task/history/snapshot atomically
    Gateway->>DB: observe publication
    Gateway-->>Client: current public task
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
