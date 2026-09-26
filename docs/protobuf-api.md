# Protobuf API conventions

Kagent-owned gRPC schemas live in
[`proto/kagent/api/v1alpha1`](../proto/kagent/api/v1alpha1). This guide describes
their implemented contracts and the conventions for extending them. The source
`.proto` files define the wire format; services and stores define authorization,
completion, and persistence behavior.

The protobuf package version is independent of the `kagent.dev/v1alpha3`
Kubernetes API. Agent is the Kubernetes definition; Session is its PostgreSQL-backed
conversation and lifecycle resource. Use the [system overview](architecture/README.md)
to choose the owning component and the [Kubernetes guide](kubernetes-api.md) for
CRD design. Upstream A2A owns task, interaction, streaming, and history semantics;
Session binds that conversation to its Agent and runtime without duplicating the
interaction model.

## API surfaces

| Schema | Responsibility |
| --- | --- |
| [agents.proto](../proto/kagent/api/v1alpha1/agents.proto) | Runnable Agent definitions with inline or referenced configuration |
| [sessions.proto](../proto/kagent/api/v1alpha1/sessions.proto) | Session creation, reads, naming, lifecycle, and shares |
| [checkpoints.proto](../proto/kagent/api/v1alpha1/checkpoints.proto) | Retained conversation boundaries, checkpoint naming, and session forks |
| [scheduled_runs.proto](../proto/kagent/api/v1alpha1/scheduled_runs.proto) | Creator-owned schedules and execution summaries |
| [harnesses.proto](../proto/kagent/api/v1alpha1/harnesses.proto), [agent_templates.proto](../proto/kagent/api/v1alpha1/agent_templates.proto) | Kubernetes configuration catalogs and mutations |
| [models.proto](../proto/kagent/api/v1alpha1/models.proto), [tools.proto](../proto/kagent/api/v1alpha1/tools.proto), [prompts.proto](../proto/kagent/api/v1alpha1/prompts.proto) | Model configuration, tool discovery and MCP apps, and prompt templates |
| [system.proto](../proto/kagent/api/v1alpha1/system.proto) | Version, current-user claims, namespaces, and Substrate inventory |
| [memory.proto](../proto/kagent/api/v1alpha1/memory.proto) | Runtime memory ingestion and retrieval |
| [task_store.proto](../proto/kagent/api/v1alpha1/task_store.proto) | Private runtime persistence of upstream A2A tasks |

The [gRPC server](../go/core/internal/grpcserver/server.go) registers services
alongside upstream A2A. Every new RPC needs an entry in the
[method policy table](../go/core/internal/grpcserver/policy.go); an unclassified
method is rejected. Policies distinguish public, read, create, update, delete,
and runtime access. Resource authorization remains in services and workflows.
TaskStore methods require runtime authority; public user and share credentials
do not grant access.

The [Session service](../go/core/internal/service/session/service.go) owns the
shared access policy for lifecycle operations and A2A interactions. A share may
select its Session's owner for lookup without replacing the authenticated
principal; read-only restrictions apply to direct service callers as well as RPCs.
Shares cannot create Sessions. Reads do not provision or wake a runtime.

## Resource and request shapes

Reuse the existing resource message across reads and mutations where appropriate,
but follow each service's schema. There is no shared `ResourceMetadata` message
or universal `metadata`/`status` envelope.

| Resource | Current shape |
| --- | --- |
| `Agent` | Kubernetes `ref` and complete `StructuredObject resource`, including inline/reference configuration and Agent status |
| `Session` | Flat identity, creator, `agent` reference, prepared revision, A2A authority and context ID, lifecycle state, failure, timestamps, and `name` |
| `Checkpoint` | Flat identity, source session, saved task/history boundary, state, failure, timestamp, and `name` |
| `ScheduledRun` | Identity, creator, immutable `agent` reference, mutable `ScheduledRunConfig config`, `etag`, and scheduling/deletion timestamps |
| `ScheduledRunExecution` | Accepted invocation inputs, trigger, session/task references, and execution summary; A2A remains authoritative for the transcript |

Session, checkpoint, schedule, execution, and share IDs are server-generated
UUIDs; treat them as opaque. Display names use `name`, not resource identity.
A Session's `context_id` equals its `id` and is its public A2A conversation ID.
Forks receive fresh Session, context, and task IDs. Route A2A through the Agent
and authorize against the resolved Session.
Kubernetes references use `ResourceReference { namespace, name }`.

Requests usually carry explicit operation inputs rather than a complete resource.
For example, `CreateSessionRequest` contains one `agent` reference, `request_id`,
and an optional display name. Its response wraps `session`. Keep method-specific
request and response messages, including empty responses where the current
method uses them.

Implement meaningful operations rather than assuming every service has full CRUD.
Agent exposes List, Get, Create, Update, and Delete; Harness exposes List, Create,
and Delete. Session naming uses `UpdateSessionName`; checkpoint naming uses
`UpdateCheckpointName`.
Suspend, Resume, Fork, and Trigger are explicit actions. Response shapes also
vary: session and schedule deletion return a resource, while checkpoint and
Kubernetes deletion return empty responses.

## Agent routing and A2A identity

HTTP/JSON-RPC selects an Agent at `/agents/{namespace}/{name}`. gRPC uses the
upstream A2A `tenant` field, `namespace/name`. An HTTP request may omit `tenant`;
if supplied, it must match the URL. Transport adapters resolve this selection
before calling the [Session interaction service](../go/core/internal/service/session/interactions.go),
which owns authorization, Session membership checks, dispatch, and observation.

A message with neither a context ID nor a task ID creates a Session for the selected Agent.
A context ID continues that Session; a task ID alone resolves its Session from
the globally unique task ID. If both IDs are supplied they must agree, and the
Session must belong to the selected Agent. A shared listing is restricted to its
one conversation. Agent Card discovery creates no Session and uses the Agent's
latest successful revision, or the pinned revision of a shared Session.

For an initial send, authenticated creator, Agent, and message ID identify the
creation retry. Repeating it reuses the Session and recovers accepted input
without redispatching it. Once context/task IDs are known, that initial-send
guarantee no longer applies: use task reads or subscriptions after an ambiguous
continuation response. See [A2A transports](architecture/a2a-transports.md) and
[gateway behavior](architecture/a2a-gateway.md) for the full contract.

## Fields and validation

- Use snake_case fields, plural collection names, and resource-specific names.
  Enum values use the enum-name prefix and an `UNSPECIFIED` zero value.
- Use `oneof` for exclusive variants. Use scalar `optional` when absence differs
  from zero, as with TaskStore's `dispatch_id` and model updates' `api_key`.
  Message fields already have presence.
- Prefer typed fields. `StructuredObject`, MCP payloads, and memory metadata have
  specific uses; they are not a reason to add arbitrary extension maps.
- Declare request-intrinsic validation in `.proto` with `buf.validate`. Prefer
  standard rules for UUIDs, lengths, ranges, required messages, and enums; use
  CEL for relationships such as requiring task and context IDs inside a
  TaskStore task payload. Do not duplicate those rules in transport handlers.
- Put authorization and checks involving stored objects or external systems in
  the owning service. Put transactional invariants in the store.

The server runs Protovalidate after authentication and before unary handlers.
Validation rules are embedded in generated descriptors; there are no generated
Go validator files. Coverage depends on the annotations present: several catalog
schemas still rely on explicit decoding and service checks. The current stream
interceptor chain has no Protovalidate interceptor; do not assume a new streaming
method inherits unary validation.

Protovalidate validates inputs; it does not apply defaults. For example,
[schedule normalization](../go/api/scheduledrun/schedule.go) supplies UTC and a
15-minute execution timeout before persistence. Document defaults where they are
applied. Clients must handle unfamiliar output enum values without interpreting
them as a known successful state.

## Updates and concurrency

Concurrency is specific to the operation:

| Operation | Input and concurrency contract |
| --- | --- |
| `UpdateSessionName` | Session ID and name; no etag. Empty clears the name |
| `UpdateCheckpointName` | Checkpoint ID and name; no etag. Empty restores the generated name |
| `UpdateScheduledRun` | Schedule ID, required UUID-shaped etag, and replacement config; stale etags return `ABORTED` |
| TaskStore `UpdateTask` | Session, task snapshot, and positive `expected_version`; conflicting updates return `ABORTED` |
| Kubernetes updates | Resource-specific adapters; see [Kubernetes objects over gRPC](#kubernetes-objects-over-grpc) |

[ScheduledRun updates](../go/core/internal/database/scheduled_runs.go) compare the
etag and replace config in one transaction, issuing a new etag on each accepted
update. The Agent reference is outside that mutable config.
Changes affect subsequently reserved executions; accepted executions retain
their inputs. The next occurrence is recalculated when schedule, time zone, or
pause state changes. Prompt and name edits do not skip an already-due occurrence.

There is no common field-mask update API or delete-etag option. Do not infer patch
semantics from nonzero fields or advertise concurrency checks a method cannot
express. A future update contract must define presence, clearing, immutable
inputs, and stale-write behavior together with its clients and store operation.

## Completion and retries

A timeout does not imply rollback. Retrying is safe only under the owning
operation's deduplication and lifecycle rules.

| Operation | Current retry identity |
| --- | --- |
| `CreateSession` | Creator and `request_id`; the Agent reference must match. Reusing the key does not rename the Session |
| `CreateCheckpoint` | Owner and `request_id`; source session and `expected_head_task_id` must match |
| `ForkSession` | Owner and `request_id`; the source checkpoint must match |
| `CreateScheduledRun` | Creator and `request_id`, checked against the original normalized creation inputs |
| `TriggerScheduledRun` | Schedule and `request_id`; retries return the same accepted execution |
| `CreateSessionShare` | No request ID or deduplication contract; each successful call creates a share and returns its token once |

Where these methods expose `request_id`, it is required, with a schema length
of 1–128. Conflicting reuse returns `ALREADY_EXISTS`. Deduplication scope is
operation-specific; do not assume separate key spaces for every RPC. Deleted
sessions and forks retain request identities and reject recreation with
`FAILED_PRECONDITION`. Schedule creation retries can return the current edited
or deleted schedule. Repeated schedule deletion returns its tombstone and retains
execution history and deduplication state.

Session creation reserves durable state before provisioning through the
[lifecycle workflow](architecture/runtime-and-lifecycle.md). Schedule triggering
reserves an execution; it does not wait for the agent's answer. Checkpoint
creation names the intended terminal task explicitly: a pending snapshot permits
retry with the same task, while an advanced conversation requires a new selection.
Every task must be terminal before a checkpoint can be captured; paused input or
authentication requests are not checkpointable. Forking rewrites public context
and task references while copying saved events. Native runtime history remains
usable under the new public IDs; no permanent task-ID translation table is added.
See [persistence and checkpoints](architecture/persistence-checkpoints-and-forks.md)
for retained state and fork behavior.

For new mutations, document whether success means configuration committed,
resource ready, or durable work accepted. Database-only transitions use
transactions. Work crossing PostgreSQL and Substrate needs durable phases,
idempotent steps, and compensating cleanup. Never hold a transaction or lock
across a network call. Authorization applies on retries as well.

## Private TaskStore contract

TaskStore stores upstream `lf.a2a.v1.Task` messages; it does not define another
task model. Every request carries `session_id`. `StoredTask.version` is opaque
and scoped to that Session. After a fork, callers must reload its new public
context/task IDs and storage versions; source-session receipts are not inherited.

`CreateTask` retries with the same task and payload return the original version.
`UpdateTask` carries the expected version, complete task snapshot, and an optional
triggering A2A event. Retrying the same expected version and payload returns the
original committed version even after later updates. The optional `dispatch_id`
fences the gateway attempt before native execution starts.

`SettleTask` acknowledges that native work and SDK cleanup have stopped at a
saved version, then publishes task state and history atomically. Settlement does
not mean the Actor has suspended or a checkpoint snapshot is ready. Runtime
authorization, versioning, and publication belong to the
[TaskStore service](../go/core/internal/service/taskstore/service.go) and store.

## Lists

Paginated control-plane methods reuse `PageRequest` and `PageResponse` from
[common.proto](../proto/kagent/api/v1alpha1/common.proto). `limit = 0` selects 50;
explicit limits are 1–100. Continue until `next_page_token` is empty, even if an
intermediate page contains no items.

Session, share, checkpoint, schedule, and execution lists use ID cursors.
Clients must treat tokens as opaque and keep filters and scope unchanged while
paging. These cursors do not encode or validate a binding to the original query;
services reapply ownership and authorization on each request. Session lists
filter by `agent` and separately authorize `all_creators`. Denied Sessions are
filtered before pagination so they consume neither result slots nor exposed
cursors; a share restricts the list to its own Session.

Substrate actor/worker lists preserve upstream ordering and continuation tokens.
Worker namespace filtering applies to each upstream page, so a filtered page
may be empty while another page exists. Inventory responses can carry
`ate_api_error` alongside partial data; callers must surface that field.

Catalog and several discovery/memory methods return unpaginated collections.
TaskStore `ListTasks` wraps the upstream A2A request and response, including A2A's
pagination fields. Do not apply `PageRequest` semantics to those methods.

## Errors

Use [serviceerrors](../go/core/internal/service/serviceerrors/errors.go) for
transport-independent failures. The
[gRPC error mapping](../go/core/internal/grpcserver/interceptors.go) preserves
existing gRPC statuses, maps service errors, and hides unexpected internal errors.

| Code | Meaning |
| --- | --- |
| `INVALID_ARGUMENT` | Malformed request or failed input validation |
| `UNAUTHENTICATED`, `PERMISSION_DENIED` | Authentication or authorization failure |
| `NOT_FOUND` | Resource absent from the authorized lookup |
| `ALREADY_EXISTS` | Identity collision or conflicting idempotency reuse |
| `FAILED_PRECONDITION` | State or dependency prevents the operation |
| `ABORTED` | Stale version or conflicting operation |
| `RESOURCE_EXHAUSTED` | Capacity or quota exceeded |
| `UNAVAILABLE` | Temporary failure; retry only under the method's guarantees |
| `CANCELLED`, `DEADLINE_EXCEEDED` | Request cancelled or deadline elapsed; durable work may already exist |
| `INTERNAL` | Unexpected server failure |

There is no universal structured error-reason envelope. Checkpoint creation does
provide `google.rpc.ErrorInfo` with domain `kagent.dev` and reasons
`KAGENT_CHECKPOINT_SNAPSHOT_PENDING` or `KAGENT_CHECKPOINT_CONVERSATION_ADVANCED`.
Use those details to distinguish retry from reselection; do not parse error prose.

## Kubernetes objects over gRPC

The Go CRD type remains the source for Kubernetes fields. `StructuredObject`
carries `api_version`, `kind`, and a `google.protobuf.Struct value`. Catalog
resources wrap it with a `ResourceReference` and selected derived fields rather
than duplicating the CRD spec in protobuf.

The [structured-object decoder](../go/api/structuredobject/structuredobject.go)
checks the expected wrapper kind, payload size, and unknown JSON fields. It does
not itself validate the API version or full CRD schema; Kubernetes admission
enforces the schema when the object is written.

Agent, Harness, and AgentTemplate create requests carry both `ref` and `resource`.
Their adapters fill missing name/namespace from `ref` and reject disagreement.
The [Agent](../go/core/internal/grpcserver/agent.go) and
[AgentTemplate](../go/core/internal/grpcserver/agenttemplate.go) update adapters
load the live object, replace spec and labels, and preserve other metadata and
any status. They do not compare caller-supplied UID/resourceVersion.
The shared [CRUD service](../go/core/internal/service/kubecrud/service.go) does
not map Kubernetes update conflicts to `ABORTED`; they currently become
`INTERNAL`. Delete requests carry only a reference, without caller preconditions,
and do not wait for finalizer completion.

These adapters do not yet implement the full stale-write protection described as
a target in the Kubernetes guide. Do not promise it to clients. Kubernetes CRUD
also has no database request-ID ledger. Prompt templates use `ref` and a typed
data map; model/tool creation can carry separate secret material, which must
remain outside public CRD specs and responses.

## Current API cutover

The Agent/Session cutover is a breaking change. `SessionService`, `ForkSession`,
and `session_id` replace the AgentInstance RPC surface and field names. Session
creation, Session filtering, and ScheduledRun creation now select one Agent;
the former Harness/AgentTemplate fields are reserved. AgentTemplate's
`admitting_harnesses` projection is also reserved; preparation belongs to Agent.

Go and Python runtime contracts reflect the new API. The checked-in browser
client and generated TypeScript still use the earlier AgentInstance contracts;
their Session API and Agent-level A2A migration remains a separate change.
Regenerating TypeScript alone does not migrate those callers.

This pre-release reset folds schema changes into `000001_initial.sql` and requires
recreating development databases. It provides no upgrade migration from the old
AgentInstance tables or persisted protobuf data. Review and deploy the controller
and its clients together.

## Generation and review

Run protobuf workflows from the repository root:

```sh
make proto-generate
make proto-lint
make proto-breaking PROTO_BREAKING_BRANCH=origin/main
make proto-check
```

`proto-check` runs lint and generation, then checks for changes from committed
generated output. Intentional schema updates also produce drift until their
generated outputs are committed, so review and include those diffs in the change.
Never edit generated files directly.

| Generated output | Source configuration |
| --- | --- |
| Go clients and servers in `go/api/gen` | [buf.gen.yaml](../proto/buf.gen.yaml) |
| Python Memory and TaskStore clients in `python/packages/kagent-proto/src` | [buf.gen.yaml](../proto/buf.gen.yaml) |
| TypeScript public API messages in `ui/src/generated`; excludes private TaskStore | [buf.gen.typescript.yaml](../proto/buf.gen.typescript.yaml) |
| Python validation descriptor dependency | [buf.gen.python-validation.yaml](../proto/buf.gen.python-validation.yaml) |

Treat `a2a.proto` and `ateapi.proto` as upstream inputs. Go uses the upstream A2A
and Substrate generated packages; TypeScript generation includes imported types.
Keep schema copies, SDK dependencies, and import mappings aligned. Follow the
[upstream schema notes](../proto/README.md) for Substrate.

Reserve removed field names and numbers. Buf uses `FILE` breaking checks but
currently ignores unstable packages, including `kagent.api.v1alpha1`; a passing
check is not proof of alpha API compatibility. Review affected Go, Python, and
browser clients, plus durable protobuf data such as schedules and executions in
PostgreSQL. Test changed validation, authorization, stale writes, retries, and
partial failures at their owning boundaries. Keep implementation plans local;
merge lasting contracts and architecture with the relevant change.
