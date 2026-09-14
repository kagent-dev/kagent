# EP-1270: Scoped authorization for configuration resources

## Summary

Kagent needs resource-scoped authorization for configuration resources without coupling its API to a particular policy system.

Authorization decisions use trusted resource identity. Unauthorized resources are omitted from collections, and unauthorized operations fail with the standard permission-denied response.

## Initial scope

| Resource type | Operations | Attributes |
| --- | --- | --- |
| `AgentTemplate` | list, get, create, update, delete | `namespace`, `name` |
| `Harness` | list, create, delete | `namespace`, `name` |
| `ModelConfig` | list, get, create, update, delete | `namespace`, `name` |

`ListConfiguredProviders` derives entries from the separate `ModelProviderConfig` resource and is not changed by this design.

## Goals

- Support partial access to the protected resource collections.
- Make authorization decisions from trusted resource attributes.
- Preserve the relationships between multiple authorization constraints.
- Keep authorization policy independent from storage and transport concerns.
- Keep the default OSS experience unchanged when no external policy integration is installed.

## Non-goals

- Define roles, policies, claims, subjects, grants, or catalog keys.
- Protect `SandboxAgent`, `AgentHarness`, `AgentInstance`, `ModelProviderConfig`, tool server, or prompt template resources.
- Expose policy-engine, SQL, Kubernetes, or other backend expressions.
- Predict authorization for UI controls.

## Authorization model

Kagent needs two forms of authorization decision:

- Whether a principal may perform an operation on a specific resource.
- Which resources a principal may receive from a collection request.

A collection decision may allow the complete collection, deny the complete collection, or describe allowed alternatives. Each alternative may constrain both namespace and name. Alternatives are combined with OR, while constraints within an alternative are combined with AND. Each constraint may allow one or more exact values.

For example, a principal may be allowed resources named `agent-a` or `agent-b` in `team-a`, as well as any resource in `shared`. The relationship between name and namespace must remain intact in the authorization decision.

Only namespace and name are supported for the initial resource set. Additional attributes or operations require a separate design decision based on a concrete authorization need.

Unsupported or invalid authorization decisions fail closed.

## Resource identity

Authorization uses identity derived from stored or validated resource data. A request reference identifies what to load; it is not trusted evidence about the resource itself.

- Reads and deletes are decided from the stored resource.
- Creates are decided from the validated proposed resource.
- Updates are decided from both the stored and proposed resource, preventing a caller from moving a resource into or out of an unauthorized scope.

## Collection behavior

A protected collection returns only resources permitted by its collection decision. Authorization is applied before sorting, totals, pagination, or response construction so unauthorized resources cannot affect observable collection behavior.

A decision that permits no resources returns an empty collection. An authorization failure or a decision that cannot be safely applied fails the request; it never broadens access.

## Client behavior

Catalog responses do not include create, update, or delete capability hints for presentation logic. Such hints duplicate policy decisions, can become stale, and couple the public API to a particular client experience.

A client may therefore display an action that the caller cannot complete. The attempted operation remains authoritative and returns permission denied. Clients should handle that response without treating it as an unexpected server failure.

## Default OSS behavior

The default OSS installation permits the complete protected collections and their existing operations. External authorization integrations may narrow that access.

Resources outside the initial scope retain their existing authorization behavior.

## Alternatives considered

- Checking items after pagination was rejected because it can produce incomplete pages and incorrect totals.
- Separate allowed-name and allowed-namespace lists were rejected because they cannot preserve required relationships between attributes.
- Backend query fragments were rejected because they couple authorization policy to storage and create an unsafe trust boundary.
- UI capability hints were rejected because the operation itself is the only authoritative authorization decision.
