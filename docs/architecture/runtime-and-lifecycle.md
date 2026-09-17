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

Create (including forks), explicit Suspend, Resume, and Delete each admit a durable
operation UUID. Fork creation loads its pinned checkpoint from PostgreSQL. Namespace
provisioning belongs to the template controller; instance creation uses the pinned
ActorTemplate's existing namespace. Read-only
preparation may run concurrently, but an atomic execution claim permits exactly one
caller to issue runtime mutations. Network work holds no database transaction or lock.
Completion atomically records the result and changes or deletes the instance.
Delayed callers observe their operation's retained result, even after newer lifecycle
operations or instance deletion. This includes callers whose delayed preparation
fails after another caller has completed: the retained operation outcome takes
precedence over their local preparation error.

An unclaimed operation can retry preparation; Delete may supersede it. Once claimed,
the operation never expires. A timeout, disconnected client, process restart, or lost
runtime response leaves the operation pending and blocks conflicting lifecycle work,
including Delete. The instance retains its revision and checkpoint pins. A runtime
success whose database completion fails also remains pending. Only the original
executor with a known successful response may finish persistence; a favorable Actor
read alone does not establish that an earlier request has stopped. There is currently
no automatic takeover or administrative unlock API for uncertain operations.

Pending observers receive an error containing the operation UUID. Completed results
are currently retained indefinitely; bounded pruning must preserve at least 24 hours
of outcomes and must never remove pending work. These records are internal, not a new
public operations API. Public requests still authorize against the instance; retaining
a Delete result does not make a fresh request for a deleted instance authorized.

Explicit suspend and resume update the logical lifecycle state. Deletion closes task
admission, stops and deletes the Actor, then removes control-plane state. The workflow
entry points are in
[`go/core/internal/service/agentinstance`](../../go/core/internal/service/agentinstance).
This serialization covers explicit lifecycle calls only: A2A, Pause, Quiesce, and
checkpoint execution still need shared ownership before multi-replica gateway use.

The unreleased schema requires a clean database. Do not overlap older binaries that
can issue lifecycle calls without operation records. PostgreSQL tests with controlled
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
