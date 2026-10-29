# Scheduled runs

`ScheduledRun` is a namespaced Kubernetes resource that invokes an Agent on a
cron schedule or through a manual trigger. Kubernetes owns its configuration;
PostgreSQL stores accepted executions and their Session links. Each execution
runs through the A2A gateway as the controller's service-account principal.

## Ownership and APIs

| State | Owner | Contents |
| --- | --- | --- |
| `ScheduledRun` | Kubernetes | Agent reference, cron expression, time zone, prompt, pause flag, timeout, and reconciliation status |
| Schedule projection | PostgreSQL, maintained by reconciliation | Resource UID/generation, accepted spec and next occurrence |
| Execution | PostgreSQL | Accepted inputs, deadline, initiator, lease, dispatch state, and Session/task links |
| Session | Control-plane Session service | Conversation identity, creator principal, pinned revision, and Agent identity and optional schedule provenance |
| Task and history | A2A | Task identity, progress, results, and transcript |

ScheduledRunService exposes CRUD through the same `ResourceReference` and
`StructuredObject` envelope as other Kubernetes resources. The CRD is the
configuration schema. Trigger and execution-history RPCs use the schedule UID,
so deleting and recreating a resource under the same name does not transfer its
history to the replacement.

A schedule makes the controller invoke its Agent, so creating or updating a
ScheduledRun through ScheduledRunService, or triggering one, also requires `get`
on the target Agent and `create` on Sessions in its namespace. Kubernetes RBAC
cannot express that check: a principal allowed to write ScheduledRuns directly
through the Kubernetes API can invoke any Agent in that namespace as the scheduler.
Grant ScheduledRun write access accordingly.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: ScheduledRun
metadata:
  name: morning-triage
  namespace: support
spec:
  agentRef:
    name: ticket-triage
  schedule: "0 9 * * 1-5"
  timeZone: UTC
  prompt: Summarize unassigned urgent tickets.
  paused: false
  executionTimeout: 15m
```

`agentRef.name` selects an existing Agent in the schedule's namespace. The Agent
owns its Harness, behavior, compilation, and runtime revisions. Several schedules
can invoke the same Agent. Schedule status reports whether scheduling is ready;
Agent status reports compilation and runtime preparation.

## Reconciliation and execution

1. Reconciliation resolves the referenced Agent and records its UID in the
   scheduling projection. A missing or deleting Agent, invalid schedule, or Agent
   without a successful revision disables new reservations. A pending or failed
   Agent replacement keeps its last successful revision available. The Ready
   condition reports `InvalidConfiguration`, `AgentNotFound`, `AgentNotReady`, or
   `MemoryNotSupported` when reservations are disabled.
2. The scheduler reserves due occurrences. Manual triggers authorize schedule
   invocation and reserve the checked generation. Acceptance atomically creates
   the execution and its Session, selecting and pinning the Agent's latest successful revision under the
   same transaction as the prompt, deadline,
   and schedule UID/generation. That Session retains the revision through the
   existing runtime lifecycle.
3. The execution controller leases work, prepares the reserved Session's runtime,
   and claims dispatch before sending the prompt through the A2A gateway. The
   Session creator is the scheduler's principal.
4. A2A assigns the task ID and persists task progress. Subsequent reconciliation
   reads that state to record the outcome or stop it at its deadline.

Cron occurrences deduplicate by schedule UID and occurrence time; manual triggers
deduplicate by schedule UID, authenticated initiator, and request ID. Retries use the accepted inputs, so
later schedule edits cannot change work already reserved. A replacement ScheduledRun
with the same name cannot adopt executions bound to the old resource UID.

The dispatch claim prevents a controller retry from sending the prompt twice.
If a response is lost, reconciliation finds the task through the original A2A
message ID, `scheduled-run/<execution ID>`. Only the scheduler may send messages
with that prefix, and only to the Session it created for the execution, so another
caller cannot plant a task that recovery would adopt. A claimed dispatch with no recorded task remains uncertain until
history appears or the deadline expires; it is not automatically sent again.

Pausing a valid schedule stops cron reservations while leaving manual triggers
available. Disabling an invalid projection prevents both kinds of new reservation.
Already accepted executions retain their inputs and remain queryable.

Removing scheduler credentials stops new dispatches but keeps the execution
controller running for cleanup. It records completed task outcomes and stops
unfinished accepted work using control-plane authority. That cleanup authority
cannot start an invocation.

### Deletion and watch scope

Before reserving cron work, the scheduler reads the live CRD through an uncached
Kubernetes reader and checks its UID, generation, deletion state, and watched
namespace. The store rechecks projection eligibility and generation in the
reservation transaction. A concurrent edit cannot reserve work against a stale
projection.

ScheduledRuns do not use a finalizer. Reconciliation retires deleted projections;
uncached live-source checks also stop new reservations after offline deletion.
Cron ignores namespaces outside the watch scope, and manual triggers return a
failed-precondition error for them. Changing watch scope does not retire a
resource. A transient Kubernetes
read error skips the reservation without retiring the schedule. Accepted
executions and retained history survive schedule deletion.

## Caller and runtime identity

| Identity | Role in a scheduled execution |
| --- | --- |
| Invocation principal | The controller service account that calls the Agent |
| Session creator | The principal recorded when the execution's Session is created |
| Runtime principal | The verified Substrate Actor bound to that Session |
| Manual-trigger initiator | The caller recorded for attribution when a trigger is accepted |

`Principal.ID` is a stable, opaque identifier shared by human, service-account,
and runtime callers. Authentication normalizes the source identity and supplies
verified claims. Authorization uses those claims and trusted resource references;
consumers compare principal IDs without parsing them. `Session.creator` stores
the creating principal's ID. It does not select a credential for subsequent calls.

Kubernetes authentication uses TokenReview and maps the verified service-account
UID under a configured cluster authority. Rotation preserves the principal ID;
deleting and recreating the service account changes it. The authority must remain
stable across restarts and distinct from other Kubernetes authentication sources
sharing the store. Trusted-proxy authentication scopes its identity claim by the
authenticated issuer; the proxy owns token verification. Insecure development
mode permits caller-selected IDs without identity assurance.

Runtime principals use the verified Actor's atespace and UID. Their
`RuntimeBinding` identifies the Session and its creator for data scoping. The
runtime authorizer restricts them to that Session and its source resource; the creator
binding does not grant the creator's permissions. Human and service-account
principals have no runtime binding.

All schedules using the controller service account share its principal and
downstream permissions. Anyone trusted to run schedule code must therefore be
trusted with that shared downstream authority. Kubernetes namespace boundaries
do not make this token namespace-specific. Grant the shared principal only
permissions appropriate for every schedule using it. Services requiring
per-Agent or per-namespace authorization should use `actorJwt` and
[identity lookup](agent-identity-lookup.md), and must not grant the shared
credential equivalent access to the protected operations. Source and execution
references provide attribution and authorization context. Manually triggering a schedule still
runs as the scheduler. A later direct invocation of its Session uses the new
caller's authenticated context.

Every Session records `agent`, with the resource reference and UID that supplied
its pinned revision. A scheduled execution additionally records `scheduledRun`,
with its trigger reference, UID, and execution ID:

```json
{
  "agent": {
    "ref": {"namespace": "support", "name": "ticket-triage"},
    "uid": "<Agent UID>"
  },
  "scheduledRun": {
    "ref": {"namespace": "support", "name": "morning-triage"},
    "uid": "<ScheduledRun UID>",
    "executionId": "<Execution UUID>"
  },
  "preparedRevision": "<Runtime revision digest>"
}
```

The store derives the Agent identity from the pinned revision. Editing, deleting, or
recreating the resource does not change existing Session provenance. Schedule
history authorization uses the saved schedule reference. A human checkpoint fork
retains the Agent and revision, has no ScheduledRun provenance, and belongs to
its new creator; schedule history permission does not expose that private conversation.

## Scheduler credentials

The controller uses separate projected service-account tokens for incoming Kagent
authentication and downstream token passthrough:

| Credential | Audience | Use |
| --- | --- | --- |
| Inbound token | `kagent-invocation` | Establish the scheduler's Kagent principal |
| Outbound token | `kagent-egress` | Supply a scheduled invocation's passthrough credential |
| Kubernetes API token | Kubernetes API audience | Access the Kubernetes API |

The inbound audience is fixed at `kagent-invocation`. The outbound audience
defaults to `kagent-egress` and is configured for the receiving service.

Inbound and outbound tokens must identify the same service-account UID. Kagent
rejects equal audiences and outbound tokens that also carry the inbound audience.
The outbound token therefore cannot be replayed as a Kagent login credential.
Downstream services verify its issuer and audience and assign its downstream
permissions.

Helm configures the requested outbound lifetime:

```yaml
config:
  scheduledRuns:
    outboundCredential:
      audience: kagent-egress
      expirationSeconds: 3600
```

Kubernetes requires at least 600 seconds and may issue a different lifetime.
Kubelet rotates the projected tokens. Each invocation captures the current
verified outbound token, so its remaining lifetime may be shorter than the
requested TTL. Further credential-dependent requests fail after expiry; a
running invocation does not adopt a rotated token. Changing the scheduler's
service-account UID prevents it from continuing executions attributed to the
previous principal.

Caller-token passthrough retains the existing runtime behavior and supported
providers described in [credential injection](credential-injection.md). Scheduled
execution supplies the outgoing service-account credential through the same path
as other callers. Actor JWT authentication is independent of that credential:
Substrate injects and refreshes it at the gateway.

[Runtime credential injection](credential-injection.md) describes header sources,
destination grants, transport behavior, and credential expiry.

## Session access and retained data

Authorization distinguishes schedule configuration (`ScheduledRun`), invocation
(`ScheduledRunInvocation`), and history (`ScheduledRunHistory`). Services combine
these checks with Session permissions:

| Session operation | Schedule permission |
| --- | --- |
| Get, list, and read history | `get` on `ScheduledRunHistory` |
| Send, continue, cancel, and rename | `create` on `ScheduledRunInvocation` |
| Delete | `delete` on `ScheduledRunInvocation` |

A caller without history access receives `NotFound`, so scheduled Sessions and
executions do not reveal that they exist. A caller who can read the history but
lacks the invocation permission receives `PermissionDenied`. Execution reads and
manual triggers follow the same rule. Listings filter unauthorized Sessions before
pagination. Stored schedule references preserve these checks after the CRD is
deleted.

Checkpoints retain the source Session's creator and scheduled provenance. Reads,
creation, and forks require source Session read access and schedule history
permission as well as the checkpoint operation's permission. Renaming or removing
a scheduled checkpoint requires the corresponding schedule invocation permission.
Retained provenance supports these checks after the source Session expires. A
fork belongs to its authenticated caller and retains the Agent identity and revision without ScheduledRun provenance.

Read-only shares remain limited to their Session and cannot invoke work. Shares
are managed by their recorded creator. Runtime access remains limited to the
Actor's verified Session binding.

Memory is partitioned by Agent key (`namespace/name`) and creator principal ID.
Every scheduled execution would share the scheduler's partition with every other
schedule targeting the same Agent, so scheduled runs do not support memory. Reconciliation
marks a schedule whose Agent's Harness enables memory as not Ready with
`MemoryNotSupported`, and the memory service rejects calls from scheduled
Sessions.

## A2A routing

All Agent conversations use the `namespace/name` tenant and the
`/agents/namespace/name` JSON-RPC and card endpoints. Scheduled conversations use
that same route. Requests naming an existing Session or task must match its
recorded Agent reference, and still pass Session and schedule authorization.
Knowing the Agent's route does not grant access to a scheduled conversation.
Accepted work remains bound to its pinned Agent UID and revision after Agent or
schedule edits and deletion; new reservations require an available Agent.

Manual-trigger and checkpoint request IDs are scoped to the authenticated
requester even though their resulting scheduled data belongs to the scheduler.
Occurrences more than thirty seconds late are skipped and logged with their
schedule ID, scheduled time, and lateness.
