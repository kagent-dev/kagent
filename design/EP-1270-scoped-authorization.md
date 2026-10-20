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

Only namespace and name are supported for scoped resource collections. Additional attributes or operations require a separate design decision based on a concrete authorization need.

Unsupported or invalid authorization decisions fail closed.

## Resource identity

Authorization uses namespace and name, the only attributes in scope. Each decision is made before the operation it authorizes.

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

After this check, WorkerPool rows use `Scope(VerbList, "WorkerPool")` and are intersected with configured and requested namespaces. A `NONE` pool scope returns no pool rows while still allowing ATE inventory under the summary capability. Invalid scopes and evaluator errors fail the request.

The summary capability grants the ATE counts and actor-template inventory included in the summary. It does not require or imply permission to use the dedicated actor and worker endpoints. Those endpoints independently require `Check(VerbGet, Resource{Type: "Substrate"})`, also with empty namespace and name. External authorizers may intentionally grant these two capabilities independently. Existing ATE atespace and worker-namespace filters remain in place; WorkerPool scope does not isolate ATE inventory.

`Scope` is reserved for `VerbList` collection filtering. Neither capability uses `Scope(VerbGet, ...)`.

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
