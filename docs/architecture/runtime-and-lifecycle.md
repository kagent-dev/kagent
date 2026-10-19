# Runtime and Lifecycle

An `Session` is PostgreSQL-backed control-plane state exposed through gRPC.
It pins one prepared revision and names one Substrate Actor. It is not a
Kubernetes resource.

## Creation and state

Creation selects the latest successful revision for the Agent, creates a deterministic Actor initially suspended, and marks the session
ready after Substrate accepts it. Readiness of the image was already established
while preparing the ate-api ActorTemplate; Session creation does not resume
an Actor merely to probe `/readyz`.

A create may name a volume source: an existing read-write-many volume by its
CSI driver and handle, and the mounts of it the session's actor gets, each a
clean relative directory of the volume at a clean absolute path outside `/data`
(the durable directory, which its snapshot tars on every commit), at most one
of them read-write: the session's own directory, whose sub-path names the
session as `${SESSION_ID}`. kagent never creates or deletes the volume. The
source is part of the session row and shows on get and list; a retry under the
same request ID must name the same source. The request's admission token is the
proof the installation's configured `VolumeAdmission` verifies for that source;
the service hands it to the admission and to nothing else, so it is never
stored or shown. Without a configured admission every create that names a
source or carries a token is refused `FailedPrecondition`, so no caller mounts
a volume of its choosing. A create without the fields is handled as before. An
`Agent` or `AgentTemplate` carries no volume: the session is the only place one
is named.

Substrate fixes an existing volume's mount paths in the ActorTemplate, which
every session of a revision shares, so every revision's template declares the
same two existing volumes: the session's own directory read-write at
`/workspace` and a shared directory read-only at `/mirrors`. A volume source
must fit that layout (its own directory at `/workspace`, optionally the mirrors
at `/mirrors`) and a volume handle of at most 256 characters, Substrate's
bound; anything else is refused `InvalidArgument`. At create the session's
Actor supplies the source's directories for them, `${SESSION_ID}` replaced by
the session's id; an Actor without a source supplies none and gets neither
mount, and its create request is unchanged. Substrate boots such an Actor from
its image rather than the golden snapshot and never creates, detaches or
deletes the volume: suspend, pause, resume and a repoint keep the mounts. A
fork starts without the source, since an Actor restored from a checkpoint
cannot carry existing volumes. Deleting a session with a source deletes only
its Actor and records a `SessionDirectoryReleased` Event on its Agent naming
the session, the volume and the directory (annotations `kagent.dev/session-id`,
`kagent.dev/csi-driver`, `kagent.dev/volume-handle`, `kagent.dev/sub-path`), for
the volume's owner to delete the directory.

Every turn's dispatch moves the session's `last_turn_time`, shown on get and
list, by which a volume's owner cleans up the directories of sessions nobody
returned to. Every dispatch also carries the turn's working directory in the
`x-kagent-working-directory` header: `/workspace` with a volume source,
`/data/workspace` without. A runtime resolves it per turn, never at boot.
A session whose source mounts the mirrors read-only also gets
`x-kagent-mirrors-directory` (`/mirrors`), and one whose admission named
repositories that moved since the mirrors' sync gets them comma-separated in
`x-kagent-changed-repositories`.

### Session directory setup

Before the first turn in a session directory with mirrors, the `claude` and
the Go ADK (`kagent` Harness) runtimes set the directory up with the shared
`go/harness/workspace`: every bare mirror `<owner>/<repository>.git` gets a
clone with alternates at `<owner>/<repository>` (`git clone --shared
--no-checkout`, a few KiB of refs; the objects stay the mirror's), its `origin`
the mirror's own origin URL without any user information, no working tree and
no local branch, with `checkout.workers`, `core.untrackedCache` and
`feature.manyFiles` set for a network filesystem. The agent's first `git switch
<default branch>` checks a repository out, from origin's branch. Then exactly
the changed repositories are fetched from origin, each under a timeout and all
under one budget, inside the dispatched turn, so the fetch leaves through the
egress gateway with whatever credential it sets for the turn; the sandbox holds
none. Notes in the directory tell the agent how it is laid out (`CLAUDE.md`
for `claude`, `AGENTS.md` for the Go ADK, whose tool descriptions point at
it), and `.kagent/workspace.json` records the setup, so a later turn neither
clones nor fetches again, and a resumed turn (a `claude` turn that continues
its process, a Go ADK turn that answers a question or an approval) sets up
nothing. A directory without the record, such as a fresh one after the
volume's owner cleaned a session's directory up, is set up again; both
runtimes tell a session with earlier turns (a Go ADK session with events, a
`claude` turn that continues an earlier conversation) so in that turn's first
status message, the one notice of `go/harness/workspace`. A
clone or fetch that fails, such as a fetch the origin refuses, is the turn's
first status message, naming the repository and the reason, and the turn runs.
A harness that runs as root, as the `claude` image does, runs the setup as
Claude's user: itself again, `kagent-claude prepare-workspace`, with that
identity, which then owns every clone and file in the directory. Root only
hands the directory over, since inside the sandbox root may not write a
directory that belongs to another user, and the session's directory on the
volume is the agent's user's. Claude Code is spawned in the turn's working
directory. The Go ADK runs as its image's own user and sets the directory up
itself, with the environment its `bash` commands get; `bash` runs in the
turn's working directory and the file tools resolve relative paths there,
with the scratch directory (`$TMPDIR/kagent/<session id>/`, `uploads/` and
`outputs/`) reachable by its absolute path. A turn whose dispatch names no
directory works in the scratch directory.

### Session volume admission

The built-in admission is `jwt` (`go/core/internal/service/session/jwtadmission`),
configured by `KAGENT_SESSION_ADMISSION_JWT_JWKS_URL`, `_ISSUER` and `_AUDIENCE`
(chart values `controller.session.admission.jwt.{jwksURL, issuer, audience}`):
all three or none, a partial configuration fails startup. The JWKS URL must be
`https`, since whoever can rewrite the keys in transit can sign grants; a plain
`http` URL fails startup unless `KAGENT_SESSION_ADMISSION_JWT_ALLOW_HTTP` (chart
`controller.session.admission.jwt.allowHTTP`) allows it, for an in-cluster
issuer that serves no TLS yet. The admission token
is a short-lived JWT signed by the installation's workspace issuer (ES256,
ES384, RS256 or PS256, with a `kid`), whose payload is:

```json
{
  "iss": "<issuer>", "aud": "<audience>", "sub": "<the caller>",
  "exp": 0, "nbf": 0,
  "agent_namespace": "<the namespace of the session's Agent>",
  "volume": {"driver": "nfs.csi.k8s.io", "handle": "<the driver's volume handle>"},
  "mounts": [
    {"sub_path": "sessions/${SESSION_ID}"},
    {"sub_path": "mirrors", "read_only": true}
  ],
  "changed": ["<owner>/<repository>"],
  "providers": [
    {"hostname": "github.com", "scheme": "Basic", "provider": "<provider instance>"},
    {"hostname": "api.github.com", "scheme": "Bearer", "provider": "<provider instance>"},
    {"hostname": "gitlab.example.com", "pathPrefix": "/api/", "scheme": "Bearer",
     "provider": "<provider instance>"}
  ]
}
```

It is admitted when the signature verifies against a key of the issuer's JWKS,
`iss` and `aud` are the configured ones, `exp` has not passed and `nbf` has
(30 seconds of leeway), `sub` is the caller as the authenticator identifies it
(`KAGENT_AUTH_USER_ID_CLAIM`), `agent_namespace` is the namespace of the
session's Agent (a grant without it is refused), the grant's volume is the
request's, and every
requested mount's sub-path is one the grant allows, read-only where the grant
says so. `changed` lists repositories, relative to the session's working
directory, whose origin moved since the volume's last sync; each provider is a
host whose credential the session gets, with the `Basic` scheme (git) or
`Bearer` (the provider's API), from the provider instance it names: one DNS
label, so it is one segment of the credential URI. A grant from before
`provider` existed names the instance as `audience`, which is read in its
place. A provider may narrow its host to the request paths under a
`pathPrefix`, a path starting and ending with `/` without empty or dot
segments, a query or percent-encoding: GitLab takes Basic for git and Bearer
for its REST API under `/api/` on one host, so its grant lists the host once
without a prefix and once with `/api/`. A host and path prefix is granted once;
a grant listing one twice, or a malformed prefix, is refused. Both are stored on the session as `providers` and `changed` and
shown on get and list; the token itself is stored and shown nowhere. Each
provider's host is allowed over HTTPS on the session's own Actor, beside its
revision's origins, on create and on every repoint: the session's directory
setup and its git and API calls reach the provider without the Agent listing
it, and the Agent's other sessions and its revision are unchanged.

Each provider's host also carries the session's caller credential: the egress
gateway replaces the `Authorization` header a request from the session's Actor
carries (any placeholder value) with the rule's prefix, `Basic ` or `Bearer `,
followed by the credential it fetches for
`ate-secret://<authority>/<provider instance>/git` (Basic) or `.../api`
(Bearer), on the provider's path prefix alone when it has one: of a host's
bindings, the gateway applies the one with the longest prefix the request path
lies under, the one without a prefix otherwise (`CredentialHeader.path_prefix`).
The authority is the installation's (`KAGENT_SESSION_CREDENTIAL_AUTHORITY`),
never the grant's: a grant naming providers is refused when none is configured,
and a Kubernetes Secret provider is refused as the authority. The gateway
resolves the URI through the credential provider it configures for that
authority (`CredentialProvider.FetchSecret`, with the Actor's verified SPIFFE
id); the URI names no person and no session, and that provider finds the
person from the Actor, which it learned was the session's when the session
started. It answers the git credential already encoded for Basic
(`base64(<user>:<token>)`) and the raw token for Bearer. The token is fetched at
the gateway: it is never in the sandbox's environment, `/data` or `/workspace`,
nor in the controller or its database, which hold only the provider's host,
scheme, path prefix and instance. A session of the same Agent without
providers, the ActorTemplate's default policy and the Agent's own credential
bindings get no such rule, and a provider on a host and header the Agent
already binds a credential to is refused when the session's policy is
compiled, whatever its path prefix.

A provider instance may verify the session Actor's own JWT instead of
answering a credential URI: on the Giant Swarm platform muster, which serves
the session's person to its own sandbox once the Actor is bound to that person
with the reserved provider `muster`. The installation configures such
instances, each with its hostname and audience
(`KAGENT_SESSION_ACTOR_JWT_PROVIDERS`), and every session admitted with a grant
gets each of them as a `Bearer` provider on its hostname, stored with the
grant's own providers; a grant naming such an instance, or a provider on its
host on any path, is refused. That provider compiles into
`CredentialHeader.actor_jwt` with the instance's audience and a 15-minute
lifetime, prefix `Bearer `: the gateway mints the JWT for the Actor that sent
the request and replaces the `Authorization` header with it, so no credential
or token is in the sandbox and nothing is fetched. The rule applies on every
origin of the host the Actor's policy allows, an Agent's in-cluster `http`
origin of it included. It needs no credential authority. A session without a
grant gets none, so its calls to that host carry what its sandbox sends.

Every refusal is `PermissionDenied` with its reason (no grant, expired, not yet
valid, another person's, another volume, a mount it does not allow, an unknown
issuer, audience or key, providers without a configured credential authority, a grant naming an installation's actor JWT provider or its host, an invalid signature, a malformed grant) and never
key material. The JWKS is fetched on the first grant, trusted for ten minutes,
and fetched again on a key id it lacks (at most every ten seconds), so a key
rotation needs no restart. An issuer whose JWKS cannot be fetched refuses
`Unavailable`, not `PermissionDenied`: the grant may well be valid, and the
caller retries.

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
the retained source snapshot. A resume that Substrate answers it cannot perform
yet — `ResourceExhausted` while the worker pool has no free worker, `Unavailable`
when a departing worker or a restarting ate-api cut it short, `Aborted` while
another operation holds the Actor — is issued again within the same attempt,
with a backoff from 500 ms to 5 s, until it goes through or the attempt ends;
the attempt then returns that answer. Every other answer ends the attempt at
once. The reads and the repoint before a resume wait the same way: while
ate-api's pods are replaced, the controller's connection answers every call
`Unavailable` until it reaches a new pod. A resume that fails while its Actor is
still suspended on the template the session records has a known outcome: the
operation ends, the session stays SUSPENDED and takes any next operation, a
delete included. A resume whose Actor moved or cannot be read keeps the
operation for the next request to continue. A resume Substrate failed for a
transient cause (a worker drained under the restore) leaves its Actor crashed
until Substrate returns it to rest; a delete that finds such a resume held, with
no attempt running, claims it and ends it once the Actor rests suspended where
the operation found it, or is gone, then deletes the session. A crashed Actor
is not a lost runtime while it holds a durable snapshot, the last one it
suspended to or the template's golden it started from: Substrate returns an
Actor its worker's departure crashed (under a resume, a suspend or a turn) to
that snapshot on the next resume, and the resume reverts one Substrate refuses
to resume (a sandbox that died under its runtime, a restore whose runtime never
came up) to it (Substrate's `RevertActor`) and resumes it once more. The
session stays usable and the next send resumes it from there (the
[A2A gateway](a2a-gateway.md)). A crashed Actor without a snapshot is lost
unless the session holds no task, which gave its runtime nothing to keep; it is
reverted the same way. A second crash is the loss, and the session records it
`FAILED` with reason `RuntimeLost`. Conflicting Session lifecycle, task, and checkpoint
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
`720h` for thirty); `0` disables idle deletion, including retries, and negative values
are rejected. There is no per-agent override or maximum TTL. The worker still
sweeps expired shares (below) with idle deletion off.

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

The same sweep deletes the session shares whose `expires_at` has passed, judged on
the controller's clock like the share token lookup. An expired share grants nothing
from its expiry on; without the sweep its row stayed until the owner revoked it or
the session was deleted, one dead row for every shared conversation nobody wrote to
again. Shares without an expiry are untouched. `kagent.session.share_expired` counts
the deleted shares (`kagent_session_share_expired_total`); a sweep that deleted any
logs `deleted expired session shares` at debug level with `count`.

## Deleted Agents

Deleting an Agent retires its definition and ends its sessions. The same sweep
deletes the sessions whose Agent name has no active definition through the
bounded deletion workflow above, with the durable reason `agent_deleted`,
whatever the idle TTL: a session's runtime does not outlive the Agent it was
created for, and the ActorTemplate of its revision is collected only once no
session references it. A deletion whose runtime work failed, or whose admission
was refused while the last turn's boundary was still being settled, stays
admitted or listed and is finished by a later sweep, never dropped. A session
whose Agent was deleted and created again under the same name before the sweep
reached it is a session of a replaced Agent, not of a deleted one: it keeps its
pinned revision, as the definition model promises, until it idles out or its
Agent is deleted for good. Retained checkpoints keep their revision pins as
before, and a checkpoint retains runnable inputs after its Agent is deleted: a
fork of it, made before or after the deletion, pins the checkpoint and is left
alone by the sweep; it runs until it idles out or is deleted, and the GC
collects the revision once the last fork and the checkpoint are gone. The sweep
never cuts a turn off: a session with a running task, or a dispatch not yet
accepted, is left for a later sweep, which deletes it once the turn ended.

`kagent.session.agent_deleted` counts the completed deletions
(`kagent_session_agent_deleted_total`); each logs `deleted the session of a
deleted Agent` at debug level with `session_id`.

## Superseded revisions

A session is prepared on one runtime revision of its agent and its Actor runs
on that revision's ActorTemplate. When the agent is re-rendered — a template
change, a Harness change, a release that renders every template anew — the
agent's current revision moves on and the session's does not: a revision
rendered by an older release lacks what the platform needs now, and its
ActorTemplate keeps what that release put there. A suspended Actor can be
moved: Substrate's `UpdateActor` accepts another ActorTemplate while the Actor
is suspended, as long as the sandbox config and the durable volumes stay the
same, and the next resume restores the Actor's data onto the new template's
golden snapshot. The workflow moves an Actor onto its agent's current revision
in the same atespace, gives it that revision's egress allowlist, and records
the session as prepared on it, so the superseded revision loses its last
reference and the runtime revision GC deletes it with its ActorTemplate.

Three paths move an Actor: a turn, under its dispatch reservation and before
the gateway dials the quiesced runtime; the Resume RPC of a suspended session;
and the controller leader's sweep every `KAGENT_REVISION_REPOINT_INTERVAL` (5m
by default; `controller.revisionRepointInterval`; 0 disables), which visits
every READY session on a superseded revision whose runtime is settled — no
turn reserved or running, no idle boundary claimed, no lifecycle operation —
so a conversation nobody writes to again does not keep the old revision alive.
The turn's move and the sweep's may meet on one Actor: whichever comes second
finds the Actor moved and records what the first has yet to. A lifecycle
operation that claims the session meanwhile wins, and the Actor goes back to
the template of the revision the session records. A template Substrate
refuses — different sandbox config, a durable volume added or changed — is
logged and the session stays on its revision; a live or paused Actor is never
moved under its turn.

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

After settlement commits, a bounded, nonblocking in-memory signal wakes the local
workers. Signals coalesce; four workers drain durable claims, handing off a wake-up
before runtime I/O so a burst can use all four workers. PostgreSQL claims coordinate
replicas. Each worker scans at startup and, when idle, every minute by default
(`KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL`). A crash between commit and signaling can
leave an actor running until a surviving replica's next recovery scan; increasing
the interval increases that delay and the time until its snapshot is available.
Ordinary committed settlements do not wait for the recovery interval.

An empty claim is followed by a check for unclaimed work on ready sessions. If
dispatch, checkpoint creation, a lifecycle operation, or a row lock blocks that
work, workers retry within one second until it becomes eligible or is superseded.
Database errors also use this short retry. Explicit lifecycle completion wakes
workers when it returns a session to READY. Suspended/deleted sessions and claimed
work do not keep the short retry active. Idle workers make one claim attempt and
one pending-work check per recovery interval, rather than querying every second.
Other background workers and connection-pool idle settings still affect whether
PostgreSQL can become idle.

Unclaimed idle work survives API restarts. A claim holds a lease (30 seconds,
in database time) that its worker renews every third of it until the boundary is
settled. A claim whose lease ran out, because its API replica stopped, crashed or
was rolled, is claimed again by another worker under a new executor ID. Losing the
worker does not prove that the suspend stopped, so the new holder does not repeat
the runtime request blindly: it settles the boundary from the Actor's state as below. The
stopped holder cannot renew, finish or release a claim taken over. Uncertain
claims still block new work, but completed results remain readable. The recorded
actor UID is checked before lifecycle calls; a same-name replacement cannot be
adopted implicitly. A session that fails, its runtime lost, releases its idle
work with the failure, claimed or not: nothing settles a boundary of a runtime
that is gone, a held claim would only refuse the session's delete, and the
holder's late outcome finds the boundary settled and records nothing.

A pause or suspend that returns an error leaves its outcome to the Actor's own
state. The claiming worker reads the Actor again, first after a second and then
with a backoff up to a minute, and keeps the claim while the Actor is still in
transition (`SUSPENDING`, `PAUSING`) or Substrate cannot answer. A `SUSPENDED`
Actor of the session with an external snapshot finishes the claim with that
snapshot, and a `PAUSED` Actor finishes a pause. A `RUNNING` Actor did not take
the request, or Substrate abandoned it (the first suspend after a worker restart
can outlive its deadline this way): the worker sends the pause or suspend once
more, fenced with a newer token, and settles its outcome the same way, so a
suspend that missed its deadline still ends with a snapshot once the Actor is
suspendable again. Any other state, a crashed or missing Actor included, or an
Actor still running after the retry, releases the claim without a snapshot: the
next turn takes the runtime as it is, a lost runtime fails the session there, and
the released boundary cannot be checkpointed. The release records why on the
session as `last_quiescence_failure` (the turn's task ID, a reason of
`SuspendFailed`, `PauseFailed` or `RuntimeUnavailable`, a message and the time),
so `GetSession` and `ListSessions` explain a turn without a snapshot; the next
boundary that records a snapshot clears it. Each read, the retry and the
resolution are logged with `session_id` and `task_id`. A worker that stops while
it settles leaves the claim to the takeover above.

The claim's records fence a stopped holder; a fencing token fences one that is
frozen (a GC pause, a node stall, a slow network) while its pause or suspend is in
flight. Every Pause, Suspend and fencing Resume a holder sends carries a token: its
executor ID and a generation from the `quiescence_fencing_generation` sequence,
which grows with every request of every holder. Substrate records the newest token
per Actor and refuses an older one with `FailedPrecondition` without acting on it.
Before a claim is released on a running Actor, its holder sends a fenced Resume
with a new generation, which records the token and leaves the runtime as it is, so
a request still in flight from the same or an earlier holder cannot pause or
suspend the Actor once the next turn is admitted. A holder whose own request is
refused this way was superseded: it stops renewing and leaves the claim to its
successor. Requests without a token, from lifecycle operations and the gateway's
resume of a turn, are admitted as before.

A Substrate whose Actor API predates the token refuses it as an unknown field
(`InvalidArgument`, protobuf tag 10001). The Substrate client then remembers that
its ate-api takes no token, logs one warning, and sends that request and every
later one unfenced: the boundary works as it did before the token, without the
fence against a frozen holder's late request.

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
    API-->>Worker: nonblocking local wake-up
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

Every model call a runtime makes names the agent it is made for and the user of
the turn, as request headers: `x-kagent-agent` and `x-kagent-agent-namespace`
carry the `AgentTemplate`'s name and namespace (the controller injects them as
`KAGENT_AGENT_TEMPLATE` and `KAGENT_NAMESPACE`; the Claude harness sends the same
two through `ANTHROPIC_CUSTOM_HEADERS`, the Codex harness through its model
provider's `env_http_headers`, or `http_headers` on Amazon Bedrock, whose built-in
provider takes no others), `x-kagent-user` the authenticated user
of the turn — the identity the controller resolved from the caller's validated
token and forwarded to the actor as metadata of the same name; absent when
there is none. A gateway between the runtime and the
model provider can attribute usage per agent and user from them, where the
network identity of the caller (a shared WorkerPool pod, an egress) says nothing
about the agent. They are an accounting identity the runtime asserts about
itself, never an authorization input.

Every Actor mounts a Substrate `DurableDir` at `/data`. Harnesses keep private
state there—local framework state, workspaces, and downloaded assets that must
survive Actor replacement. This state is runtime-private; public task history
remains in PostgreSQL.

Substrate v0.5.0-alpha2 applies one content scope to every snapshot of an Actor,
both the node-local snapshot taken on pause and the one uploaded on suspend.
Templates use Full, so paused and suspended Actors keep their process memory, and
their checkpoints cannot be forked until Substrate's lifecycle v2 separates the two
scopes again. A Data snapshot resumes by starting fresh containers from the OCI
image with the saved durable directories; Substrate still restores one as Data when
the Actor's template changed after the snapshot was taken.

The Go ADK opens and migrates its SQLite session store before readiness, but
retains no idle database connections. Full and golden restores preserve guest
memory while rematerializing `/data`, so a connection opened before the snapshot
can retain a stale file identity and reject writes with `SQLITE_READONLY_DBMOVED`.
Closing connections when returned to the pool keeps quiescent snapshots free of
database handles; each later operation opens the current backing file.

A Session can bind a provider whose instance verifies the Actor's own JWT
(`KAGENT_SESSION_ACTOR_JWT_PROVIDERS`): the egress gateway replaces the
`Authorization` header of every request from the Session's Actor to that host
with a JWT it mints for the Actor. Every turn of such a Session names those
hosts to the runtime as `x-kagent-actor-jwt-hosts` (comma-separated; a provider
under a path prefix is not named). A runtime that forwards the caller's
credential on MCP calls sends a placeholder bearer to a server on one of them
instead, and holds no caller credential for a turn whose every such server is
on one: the caller's bearer never reaches the sandbox's MCP traffic there.
The gateway replaces only an `Authorization` header the request already
carries, so every harness sends the placeholder to a server on one of those
hosts that sets no `Authorization` of its own, whether or not it forwards the
caller's credential (`workspace.ActorJWTHeaders`). The Claude harness writes
such a server into the turn's MCP configuration with its own URL and the
placeholder, unless the forwarder enforces a tool selection on it, so a turn
whose every server is on one leaves no loopback token in the sandbox. A server
with a tool selection stays on the forwarder, which sends the placeholder only
while it holds the caller's credential.

## Forwarder refusal metrics

A runtime that fronts its MCP servers with a loopback forwarder (the Claude
harness) reports, on the status message of every turn boundary, what the
forwarder answered itself since the previous boundary
(`kagent.dev/a2a/forwarder-refusals`: `unauthorized`, `unknownServer`,
`refusedPath`). The task store counts them when it publishes the boundary, which
happens once per boundary, so a retried settlement counts nothing twice.

| OTel metric | Instrument / unit | Prometheus name | Meaning |
| --- | --- | --- | --- |
| `kagent.harness.forwarder.refusals` | Counter / `{request}` | `kagent_harness_forwarder_refusals_total` | Requests refused by the forwarder, with `k8s.namespace.name` and `gen_ai.agent.name` of the Session's Agent and `http.response.status_code`: `401` without the forwarder's token, `404` for a server it does not front, `400` for a path that could leave its server. |

The model client holds the forwarder's token and reaches only its servers, so a
count is another process in the sandbox probing the forwarder. The runtime
reports its own refusals: the counter is a signal to alert on, not an
attribution a process in the sandbox could not influence. A boundary settled by
the controller for a runtime that was lost before settling reports nothing.

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
