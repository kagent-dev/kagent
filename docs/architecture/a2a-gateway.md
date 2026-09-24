# A2A Gateway

The public gateway implements the upstream A2A handler for message send/stream,
task get/list/cancel, and subscription. It also serves the extended Agent Card
compiled into the instance's prepared revision.

## Routing and execution

Authentication establishes AgentInstance authority. The gateway
loads the instance and prepared revision, derives the private Actor route, and
forwards upstream A2A requests. Actor addresses and runtime credentials remain
internal.

The instance route (`x-kagent-agent-instance-id`) selects authorization and history.
`AgentInstance.context_id` is the bound A2A context, not the instance ID. Sends
may omit context to resolve that binding; a different nonempty context is rejected.
ListTasks may omit the context filter. Forks retain protocol IDs under distinct
instance routes, including for cancellation and subscription.

The runtime owns execution and persists updates through the private gRPC
`TaskStoreService`. The gateway owns each caller's observation connection.
Disconnecting a client or gateway does not cancel the native runner or remove
its persistence writer. Public authentication and authorization stay in the
API/gateway; the runtime SDK serializes execution and continues waiting tasks.

```mermaid
flowchart LR
    CLIENT[client] --> GATEWAY[authorized public A2A gateway]
    GATEWAY -->|send / cancel / live subscribe| RUNTIME[agent A2A runtime]
    RUNTIME -->|private TaskStore gRPC| API[API]
    API --> DB[(PostgreSQL)]
    GATEWAY -->|stored get / list| DB
    API -->|settled boundary| WORKER[lifecycle finalization]
    WORKER --> SUBSTRATE[Substrate pause / suspend]
```

## Durable ordering

The SDK creates a task through CreateTask and applies versioned UpdateTask saves.
Native work waits for the initial active task save, so lifecycle operations cannot
miss an execution that has already started. Immutable storage mutation receipts
allow lost save responses to be retried without duplicating history. Both SDKs persist intermediate updates
before streaming them. Public task history remains synthesized from the retained
messages and artifacts; this is not an exact replay archive of every wire event.

The final waiting or terminal save is staged until native cleanup is complete.
The runtime acknowledges that exact version, then an independent API worker
claims and performs the pause/suspend. The store publishes task state, history,
and the snapshot reference atomically. The gateway closes its runtime observation
connection before waiting for publication, so it cannot prevent suspension.
Uncertain issued lifecycle work stays claimed and blocks new execution.

The persistence model enforces:

- one non-quiescent task per instance history;
- task-ID uniqueness and optimistic version checks;
- idempotent retries of the same storage mutation; and
- an exact snapshot identity and history sequence at each quiescent boundary.

Tasks contain current materialized A2A state. Complete message history is rebuilt
from ordered event rows, not stored as one history blob.

The implementation is in
[`go/core/internal/a2agateway`](../../go/core/internal/a2agateway).

## Runtime SDK adapters

Go uses the SDK's local execution manager with `MaxExecutions: 1`. Cancellation
uses its separate path, so it remains available while execution occupies the slot.
The SDK's cluster workqueue is not enabled: with a single execution slot, that
queue also rejects cancellation while work is running.

Python (minimum SDK 1.1.5) uses the SDK's request queues and an `asyncio.Lock` to
serialize native work across tasks in one actor. Busy requests are rejected at
the handler; simultaneous requests that passed that check serialize at the lock.
Its `save(task)` adapter tracks expected versions per request and chooses ordinary
CreateTask or UpdateTask. Cancellation refreshes its version when it takes over
the SDK writer, after any in-flight execution save. Waiting task state is copied before the SDK mutates its
cache, preserving the original question for native approval/input validation.

Both adapters persist an initial active event before native work, stop execution
on persistence failure, and wait for native cleanup before settling a boundary.
Go uses the SDK cleanup callback; Python withholds the final event until its native
runner returns. Cleanup/snapshot finalization is separate from SDK task creation.
SDK upgrades must run the real gRPC/PostgreSQL fixtures covering failed saves,
cancellation, continuation, disconnects, and slow subscribers.

## Reconnect and client behavior

Get/List serve committed state without waking the runtime. Subscribe returns
current stored state for quiescent tasks; otherwise the runtime supplies its
initial task followed by live updates. If completion races subscription setup,
the gateway recovers the committed public result. Unary sends also recover the
task containing the input if suspension interrupts the response; ambiguous message
IDs and protocol errors are not recovered. Cancellation recovers only a terminal
task. An interrupted request with unfinished work
still returns an error. Clients can replace their
projection with that current task and apply subsequent upstream A2A updates.
There is no event cursor or promise of replaying every previous token event.
Public sends do not guarantee idempotency by message ID. Once a task ID is known,
use GetTask or SubscribeToTask to reconnect. Do not automatically resend an
ambiguous request: a second send may start another task or another turn. Storage
RPC retries are internal to the runtime adapter and have a separate guarantee.

The scheduler records its single dispatch attempt before sending. After an
uncertain send it only looks for the task in stored history; it never sends again.
A crash between claiming and sending can therefore leave an execution unresolved
until its deadline, when normal timeout cleanup runs.

Codex and Claude reserve their native session while approval/input is pending.
Their local executor interceptor rejects input for another task before SDK
execution. Other harnesses retain their existing policy for parked tasks. All harnesses permit at most one active execution.

## Runtime authority and deployment prerequisite

TaskStore requests require the Substrate actor JWT described in
[Substrate #1660](https://github.com/agent-substrate/substrate/issues/1660).
The API verifies the trusted issuer, signature, audience, expiry, atespace,
actor name, and recorded actor UID. User and share credentials cannot grant
private persistence access. The runtime rereads its projected actor name for
routing after restore; it never issues or refreshes runtime credentials itself.

Production injection and issuer/JWKS wiring are pending upstream. The default
API rejects private TaskStore requests until that authenticator is configured;
the worktree's signed-token injection fixture is test-only. Live Substrate
restore and snapshot conformance passes with the isolated test identity below;
production actor credential injection and refresh still require verification.
Push notifications are outside this cutover and are rejected by the public gateway.


For isolated E2E testing while Substrate actor JWT injection is pending, set
`KAGENT_INSECURE_TASK_STORE_AUTH=true` on both the API (`controller.env`) and
runtime (`Harness.spec.env`). Go and Python then send an unsigned
`x-kagent-insecure-runtime-identity` header from the projected atespace, actor
name and UID. The API still checks the recorded instance authority and UID;
public authentication is unchanged. This identity can be forged, so the switch
is default-off and must only be used in an isolated test deployment.
