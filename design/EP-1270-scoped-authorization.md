# EP-1270: Scoped authorization for Kagent resources

## Summary

Kagent needs resource-scoped authorization for configuration resources, owned records, and runtime inventory without coupling its API to a particular policy system.

Authorization decisions use trusted resource identity. Unauthorized resources are omitted from collections, and unauthorized operations fail with the standard permission-denied response.

## Protected resources

| Resource type | Operations | Attributes |
| --- | --- | --- |
| `Agent` | list, get, create, update, delete | `namespace`, `name` |
| `AgentTemplate` | list, get, create, update, delete | `namespace`, `name` |
| `Harness` | list, create, delete | `namespace`, `name` |
| `SandboxTemplate` | list, create, delete | `namespace`, `name` |
| `ModelConfig` | list, get, create, update, delete | `namespace`, `name` |
| `ModelProviderConfig` | list configured providers, get cached models, update to refresh models | `namespace`, `name` |
| `ToolServer` | list servers and tools, create, delete, MCP operations | `namespace`, `name` |
| `PromptTemplate` | list, get, create, update, delete prompt libraries | `namespace`, `name` |
| `WorkerPool` | list rows within the system summary | `namespace`, `name` |
| `SubstrateActorTemplate` | list rows within the system summary | `atespace`, `name` |
| `SubstrateActor` | list, summary counts | `atespace`, `name` |
| `SubstrateWorker` | list, summary counts | `namespace`, `name` |

`ListConfiguredProviders` applies the `ModelProviderConfig` list scope before projecting ready providers. Cached provider-model reads require get on the exact provider config; refresh requires update before invoking discovery. Session shares cannot refresh models, even when the authorizer permits update.

Tool discovery applies the `ToolServer` list scope to exact owning-server identities. Flat tool lists contain only tools belonging to those same visible servers. MCP tool discovery and resource reads require get on the referenced server; tool invocation requires create. Prompt-library identity is the namespace and name of its backing ConfigMap, rather than an individual prompt key.

Static provider definitions and supported tool-server types contain no configured resource and remain available after authentication.

## Goals

- Support partial access to the protected resource collections.
- Make authorization decisions from trusted resource attributes.
- Preserve the relationships between multiple authorization constraints.
- Keep authorization policy independent from storage and transport concerns.
- Keep the default OSS experience unchanged when no external policy integration is installed.

## Non-goals

- Define roles, policies, claims, subjects, grants, or catalog keys.
- Extend this configuration-resource contract to legacy `SandboxAgent` or `AgentHarness` resources.
- Filter ATE actor, worker, or actor-template inventory using Kubernetes object scopes.
- Expose policy-engine, SQL, Kubernetes, or other backend expressions.
- Predict authorization for UI controls.

## Authorization model

Kagent needs two forms of authorization decision:

- Whether a principal may perform an operation on a specific resource.
- Which resources a principal may receive from a collection request.

A collection decision may allow the complete collection, deny the complete collection, or describe allowed alternatives. Each alternative may constrain both namespace and name. Alternatives are combined with OR, while constraints within an alternative are combined with AND. Each constraint may allow one or more exact values.

For example, a principal may be allowed resources named `agent-a` or `agent-b` in `team-a`, as well as any resource in `shared`. The relationship between name and namespace must remain intact in the authorization decision.

Kubernetes collection scopes support namespace and name. Substrate actors and actor templates use atespace and name; workers use namespace and name as defined below. Additional attributes or operations require a separate design decision based on a concrete authorization need.

Unsupported or invalid authorization decisions fail closed.

## Resource identity

Configuration object authorization uses namespace and name. Owned records use
the resource identities defined in the Sessions, checkpoints, shares, and
scheduled runs section below. Collection authorization uses the trusted identity attributes listed for each resource type. Each decision is made before the operation it authorizes.

- Reads and deletes are decided from their references before the read, so denial does not reveal whether the resource exists.
- Creates are decided from the validated proposed resource.
- Updates authorize get before the read and update before the write. Generic updates use the identity of the loaded resource.

A service that writes through its own client authorizes update from the request reference before its first side effect.

## Collection behavior

A protected collection returns only resources permitted by its collection decision. Authorization is applied before sorting, totals, pagination, or response construction so unauthorized resources cannot affect observable collection behavior.

A decision that permits no resources returns an empty collection. An authorization failure or a decision that cannot be safely applied fails the request; it never broadens access.

## Sessions, checkpoints, shares, and scheduled runs

These services use `Authorizer.Check` for operation capabilities, not collection
`Scope` expressions. Resource namespaces are empty unless explicitly stated below.
Storage ownership remains an independent restriction: a policy grant alone does
not confer access to another user's records.

For creation of a new Session (`create Session` with empty `Name`), `Namespace`
is the target Agent's namespace. This creation context is taken from the validated proposed Agent reference,
or the Agent resolved from the caller-owned checkpoint for a fork. The same
Agent is independently authorized with `get Agent` and used for creation.
Schedules supply their stored target Agent for retries, updates, and triggers.
Session reads and other existing record operations retain their ID-based identity.
The namespace does not make Sessions Kubernetes resources or change ownership.
Category-only authorizers may ignore this creation namespace and continue checking
Session manage permission; they need not expose namespace as a Session catalog
attribute. The `create ScheduledRun` check itself remains category-level.

Target Agent access is checked when a Session is created, including checkpoint
forks. It is an admission prerequisite, not a continuous grant for the lifetime of
the conversation. Follow-up sends (including `invoke_session`) and Resume require
their current Session permissions and ownership, without rechecking `get Agent`.
Revoking Agent access prevents new conversations but does not stop existing ones;
revoke Session permissions to stop further sends or resumes. Schedule updates and
manual triggers separately recheck target grants as described below.

Session and Checkpoint API UUID inputs are normalized to canonical lowercase hyphenated form
before authorization and storage access, including source Session IDs and
share IDs and pagination boundaries.
Equivalent textual encodings cannot bypass exact-identity policy rules.


| Operation | Required checks (`verb Resource`, with `name`) |
| --- | --- |
| List Sessions | `list Session`, empty name; `get Session`, Session ID, for every returned row |
| List Sessions across creators | Above checks plus `list SessionAllCreators`, empty name |
| Create Session | `create Session`, target Agent namespace and empty name; `get Agent`, target namespace/name |
| Create Checkpoint | `create Checkpoint`, empty name; `get Session`, source Session ID |
| List Checkpoints | `list Checkpoint`, empty name; storage filters by caller ownership and source Session ID |
| Fork Checkpoint | `get Checkpoint`, checkpoint ID; `get Session`, saved source ID; `create Session`, target Agent namespace and empty name; `get Agent`, target namespace/name resolved from the owned checkpoint |
| List Session shares | `list Session`, `<session-id>/shares`; storage requires ownership |
| Create Session share | `create Session`, `<session-id>/shares`; storage requires ownership |
| Revoke Session share | `delete Session`, `shares/<share-id>`; storage requires ownership |
| List ScheduledRuns | `list ScheduledRun`, empty name; storage filters by creator |
| Create ScheduledRun | `create ScheduledRun`, empty name; `get Agent`, target namespace/name; `create Session`, target Agent namespace and empty name |
| Update ScheduledRun | `update ScheduledRun`, schedule ID; same target checks as create |
| Manually trigger ScheduledRun | `create ScheduledRun`, `<schedule-id>/executions`; same target checks as create |
| List scheduled executions | `list ScheduledRun`, `<schedule-id>/executions`; storage filters by creator |
| Get scheduled execution | `get ScheduledRun`, `executions/<execution-id>`; storage requires ownership |

Existing integrations that grant `get` for these collection operations must grant
`list` instead. Individual Session, Checkpoint, and ScheduledRun reads still use
`get` with the record ID. The all-creators Session capability does not bypass
per-Session read authorization. Checkpoint creation retries and forks authorize the
saved source identity without requiring that source Session to still exist;
retained checkpoints can outlive it.

A share recipient is authorized as the authenticated recipient, not as the owner.
They need `get Session` on the shared Session ID in addition to a valid share.
The share supplies the storage owner and permits only that Session's read paths.
Listing through a share returns at most that Session under its `get` check; it
does not require or grant the ordinary `list Session` or all-creators capability.
Shares grant no checkpoint or schedule access, and actors cannot use share credentials.

Actor runtime credentials do not grant checkpoint creation, including for their
own Session. Checkpoint management requires an independently authorized user/API
caller. Granting Session send permission does not grant `create Checkpoint`.
Creating a fresh checkpoint also requires every task to be terminal and the native
snapshot boundary to be settled; an actor cannot checkpoint its own active tool
call by gaining an additional permission.

### Scheduled execution is durable delegation

Creating a ScheduledRun delegates future automatic execution to the control plane.
Automatic dispatch uses `ControlPlaneSession`, whose principal has no human user
or claims. It does not reconstruct or re-evaluate the creator's current grants.
Revoking the creator's `get Agent` or `create Session` permission therefore does
not itself revoke an existing schedule. Control-plane authorization checks still
apply independently; an external authorizer may allow or deny those calls.

Create (including retries), update, and manual trigger recheck the caller's target
Agent and Session-create permissions. Automatic dispatch does not. Stopping future
runs requires pausing or deleting the schedule through an authorized operation.
Update, including pause, requires the target grants; delete requires its own
ScheduledRun delete grant and ownership, without target grants. This distinction
is intentional; dispatch-time creator revocation would require a different
identity and delegation contract.

## Summary and Substrate capabilities

`ListNamespaces` requires `Check(VerbGet, Resource{Type: "SystemNamespaces"})`
before reading namespace inventory, including configured watched namespaces.
This whole-inventory capability is independent of object collection scopes.

`GetSubstrateSummary` first requires `Check(VerbGet, Resource{Type: "SystemSummary"})`, with empty namespace and name. This is a whole-summary capability that policies may grant to operators or administrators. Denial fails the request with permission denied before any Kubernetes or ATE reads; the service does not return an apparently empty inventory for denied access.

The capability permits access to the summary endpoint; each inventory family then
requires its own `Scope(VerbList, resourceType)` decision:

| Resource type | Scope attributes | Trusted identity |
| --- | --- | --- |
| `WorkerPool` | `namespace`, `name` | Kubernetes object metadata |
| `SubstrateActorTemplate` | `atespace`, `name` | Substrate template metadata |
| `SubstrateActor` | `atespace`, `name` | Substrate actor metadata |
| `SubstrateWorker` | `namespace`, `name` | `worker_namespace` and worker metadata name; neither the pod name nor the pool name |

Scopes are intersected with the controller's watched namespaces and the caller's
requested namespace or atespace. A `NONE` scope skips reads for that family and
contributes no rows, counts, or status totals. Missing or blank inventory identities
are excluded even under `ALL`. Invalid scopes and evaluator errors fail the request.
WorkerPool scope does not grant access to any Substrate family.
Actor list rows expose worker assignments only when the independent SubstrateWorker
scope permits the assignment's worker namespace and global worker metadata name,
and that namespace is watched. Worker pod names and pool names do not establish
worker identity. Otherwise the assignment and assigned node are omitted; Actor
lifecycle state remains visible. Missing worker identities are redacted, and
invalid or failed worker scope decisions fail the Actor list request.
Snapshot locality is always omitted from Actor inventory: it names a node but
has no worker identity to authorize, and may refer to an earlier placement.


Dedicated actor and worker endpoints request `Scope(VerbList, "SubstrateActor")`
and `Scope(VerbList, "SubstrateWorker")`, respectively. They do not require
`SystemSummary` or the former `Substrate` capability. A `NONE` scope returns
permission denied on these dedicated endpoints. External authorizers may grant
summary access and each inventory family independently.

Public inventory cursors contain only the last returned identity and the listing
scope. Authorization and namespace filters apply before filling a page. An extra
authorized row establishes whether a continuation exists. On continuation, the
Substrate adapter encodes a keyset token for the last returned identity, avoiding
rescans of previous pages. Backend scan cursors remain internal. Each request
re-evaluates authorization; the boundary row need not still exist or be authorized.
The adapter's encoder is coupled to the pinned Substrate token format and must be
checked on upgrades. No shared cursor storage or encryption key is required. Dedicated list requests have a 10,000-row and 100-backend-page
budget shared across watched atespaces and a 10-second overall deadline (or the
caller's earlier deadline). The first backend batch requests `limit + 1` rows;
subsequent batches request up to 1,000 rows, capped by the remaining row budget.
All fetched rows count, including denied rows, lookahead, and unused rows in a
batch. The first-batch rule applies once per public request, across atespaces.
Lookahead consumes the same budget. Exhaustion yields
an explicit `ate_api_error` with `ResourceExhausted` or `DeadlineExceeded`, no rows,
and no continuation; it must not appear as successful end-of-list. Sparse scopes
may therefore fail to fill a page within the budget. Operator summary walks retain
their separate 10,000-page bound.

`Scope` is reserved for `VerbList` collection filtering. Whole-endpoint capabilities
use `Check(VerbGet, ...)`, never `Scope(VerbGet, ...)`.

## Client behavior

Catalog responses do not include create, update, or delete capability hints for presentation logic. Such hints duplicate policy decisions, can become stale, and couple the public API to a particular client experience.

A client may therefore display an action that the caller cannot complete. The attempted operation remains authoritative and returns permission denied. Clients should handle that response without treating it as an unexpected server failure.

## Default OSS behavior

The default OSS installation permits the complete protected collections and their existing operations. External authorization integrations may narrow that access.

Resources outside this scope retain their existing authorization behavior.

## Alternatives considered

- Checking items after pagination was rejected because it can produce incomplete pages and incorrect totals.
- Separate allowed-name and allowed-namespace lists were rejected because they cannot preserve required relationships between attributes.
- Backend query fragments were rejected because they couple authorization policy to storage and create an unsafe trust boundary.
- UI capability hints were rejected because the operation itself is the only authoritative authorization decision.
