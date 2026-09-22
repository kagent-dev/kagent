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
- Embed authorization capability hints in catalog resources.

## Authorization model

Kagent needs three forms of authorization decision:

- Whether a principal may perform an operation on a specific resource.
- Which resources a principal may receive from a collection request.
- Whether an advisory client action is currently permitted for an exact resource or for any valid resource name in a namespace.

A collection decision may allow the complete collection, deny the complete collection, or describe allowed alternatives. Each alternative may constrain both namespace and name. Alternatives are combined with OR, while constraints within an alternative are combined with AND. Each constraint may allow one or more exact values.

For example, a principal may be allowed resources named `agent-a` or `agent-b` in `team-a`, as well as any resource in `shared`. The relationship between name and namespace must remain intact in the authorization decision.

Only namespace and name are supported for the initial resource set. Additional attributes or operations require a separate design decision based on a concrete authorization need.

Unsupported or invalid authorization decisions fail closed.

## Resource identity

Authorization uses identity derived from stored or validated resource data. A request reference identifies what to load; it is not trusted evidence about the resource itself.

- Reads and deletes are decided from the stored resource.
- Creates are decided from the validated proposed resource.
- Updates are decided from the stored resource and preserve its namespace and name.

## Collection behavior

A protected collection returns only resources permitted by its collection decision. Authorization is applied before sorting, totals, pagination, or response construction so unauthorized resources cannot affect observable collection behavior.

A decision that permits no resources returns an empty collection. An authorization failure or a decision that cannot be safely applied fails the request; it never broadens access.

## Advisory access review

An access review with a resource name uses the same exact authorization check as
the corresponding operation. GET requires a name. A target without a name is an
existential question:
whether the complete authorization scope contains at least one valid resource name
in that namespace. It is not an exact check with an empty or wildcard name.

An authorizer that cannot produce a complete scope fails the namespace-only
review rather than returning partial results. Reviews are advisory; every resource
operation authorizes its actual input again.

## Client behavior

Catalog responses do not include create, update, or delete capability hints. Such hints duplicate policy decisions, can become stale, and couple the resource API to a particular client experience. Clients may request separate advisory access reviews when they need early permission signals.

A client may therefore display an action that the caller cannot complete. The attempted operation remains authoritative and returns permission denied. Clients should handle that response without treating it as an unexpected server failure.

## Default OSS behavior

The default OSS installation permits the complete protected collections and their existing operations. External authorization integrations may narrow that access.

Resources outside the initial scope retain their existing authorization behavior.

## Alternatives considered

- Checking items after pagination was rejected because it can produce incomplete pages and incorrect totals.
- Separate allowed-name and allowed-namespace lists were rejected because they cannot preserve required relationships between attributes.
- Backend query fragments were rejected because they couple authorization policy to storage and create an unsafe trust boundary.
- Capability hints were rejected because the operation itself is the only authoritative authorization decision.
