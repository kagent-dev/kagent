# EP-1270: Scoped authorization for configuration resources

## Summary

Kagent needs resource-scoped authorization for configuration resources without coupling its API to a particular policy system.

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
- Extend this configuration-resource contract to legacy `SandboxAgent` or `AgentHarness` resources or to `Session` authorization.
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

Object authorization uses namespace and name. Collection authorization uses the trusted identity attributes listed for each resource type. Each decision is made before the operation it authorizes.

- Reads and deletes are decided from their references before the read, so denial does not reveal whether the resource exists.
- Creates are decided from the validated proposed resource.
- Updates authorize get before the read and update before the write. Generic updates use the identity of the loaded resource.

A service that writes through its own client authorizes update from the request reference before its first side effect.

## Collection behavior

A protected collection returns only resources permitted by its collection decision. Authorization is applied before sorting, totals, pagination, or response construction so unauthorized resources cannot affect observable collection behavior.

A decision that permits no resources returns an empty collection. An authorization failure or a decision that cannot be safely applied fails the request; it never broadens access.

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

Dedicated actor and worker endpoints request `Scope(VerbList, "SubstrateActor")`
and `Scope(VerbList, "SubstrateWorker")`, respectively. They do not require
`SystemSummary` or the former `Substrate` capability. A `NONE` scope returns
permission denied on these dedicated endpoints. External authorizers may grant
summary access and each inventory family independently.

Public inventory cursors contain only an offset into the authorized result. Backend
cursors, which can encode denied identities, remain internal. Each continuation
re-evaluates authorization and scans from the beginning, filtering before counting
the offset or filling the page. This bounds retained memory to a page and works
across replicas without shared cursor state, at the cost of repeated backend reads
on later pages. As with other live collections, changes between requests may shift
page boundaries; cursors do not represent a snapshot.

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
