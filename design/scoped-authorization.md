# Scoped Authorization for Kagent Configuration Resources

Status: Draft

## Summary

Kagent uses `Authorizer.Check` for resource authorization.

This design adds scoped authorization for `AgentTemplate`, `Harness`, and `ModelConfig` resources.

Kagent supplies trusted resource attributes and enforces authorization before it builds a response.

An `Authorizer` implementation defines roles, policies, identity rules, and catalog keys.

The protected RPC responses will also report the actions that the caller can use.

## Initial scope

| Resource type | Operations | Attributes |
| --- | --- | --- |
| `AgentTemplate` | list, get, create, update, delete | `namespace`, `name` |
| `Harness` | list, create, delete | `namespace`, `name` |
| `ModelConfig` | list, get, create, update, delete | `namespace`, `name` |

`ListConfiguredProviders` derives entries from the separate `ModelProviderConfig` resource. This design does not change its authorization.

Provider discovery does not return `ModelConfig` resources. This design does not change its authorization.

## Goals

- Support partial access to the three protected resource collections.
- Apply authorization before sorting and response construction.
- Use trusted stored or validated resource attributes.
- Keep authorization rules outside storage code.
- Keep the extension contract independent from one policy system.

## Non-goals

- This design does not define roles, policies, claims, or catalog keys.
- This design does not protect `SandboxAgent`, `AgentHarness`, `AgentInstance`, or `ModelProviderConfig` resources.
- This design does not protect tool servers or prompt templates.
- This design does not add SQL or backend expressions to the authorization API.

## Authorization API

Kagent will keep the current `Authorizer.Check` interface.

The `Resource` type will carry trusted attributes:

```go
type Resource struct {
	Type       string
	Name       string
	Attributes map[string][]string
}
```

Services will construct attributes from stored or validated resource data.

Kagent will use `CollectionAuthorizer` for protected collection operations:

```go
type CollectionAuthorizer interface {
	Authorizer
	Scope(
		ctx context.Context,
		principal Principal,
		verb Verb,
		resourceType string,
	) (AuthorizationScope, error)
}
```

An `AuthorizationScope` describes the required collection restriction:

```go
type ScopeKind string

const (
	ScopeAll   ScopeKind = "ALL"
	ScopeNone  ScopeKind = "NONE"
	ScopeAnyOf ScopeKind = "ANY_OF"
)

type ScopeOperator string

const (
	ScopeIn ScopeOperator = "IN"
)

type AuthorizationScope struct {
	Kind  ScopeKind
	AnyOf []ScopeClause
}

type ScopeClause struct {
	All []ScopePredicate
}

type ScopePredicate struct {
	Attribute string
	Operator  ScopeOperator
	Values    []string
}
```

`ScopeAll` permits the complete collection. `ScopeNone` permits no items.

`ScopeAnyOf` joins clauses with OR. Each clause joins predicates with AND.

`ScopeIn` matches a listed value.

The initial protected attributes, `namespace` and `name`, are always present and non-empty. An absent-attribute operator would therefore describe a state these resources cannot produce. Add another operator only when a protected resource introduces an attribute whose absence has authorization meaning.

The scope contains no SQL, Kubernetes field paths, policy types, or backend expressions.

## Response capabilities

Each protected list response will include `can_create`.

Each returned `AgentTemplate` and `ModelConfig` will include `can_update` and `can_delete`.

Each returned `Harness` will include `can_delete` because the service has no update RPC.

Kagent will calculate these fields from `AuthorizationScope` values for each action.

The fields help a client control its actions. They do not replace authorization on an RPC.

## Single-resource enforcement

For a read, the service will load the stored resource before authorization.

For a create, the service will authorize the validated proposed resource.

For an update, the service will authorize the stored and proposed resources.

For a delete, the service will load and authorize the stored resource.

The service will use `namespace` and `name` from the Kubernetes object metadata.

The service must not trust a request reference as stored resource data.

## Collection enforcement

For each protected collection request:

1. Validate the caller query.
2. Request the `AuthorizationScope` with `VerbList`.
3. Query a safe Kubernetes resource set.
4. Apply the scope to trusted object metadata.
5. Sort the authorized items.
6. Build the response from the authorized items.

`ScopeNone` returns an empty protected collection.

An authorization error fails the request. Kagent must not convert an error to `ScopeAll`.

The matcher will accept only `namespace` and `name` for these resources.

The matcher will reject an unsupported scope kind, attribute, operator, or empty value.

The services will pass the scope through an explicit function argument. They will not store it in request context.

The current Kubernetes lists do not use server pagination. A complete in-memory filter is correct for the initial release.

If a list adds pagination, it must apply the scope before totals, sorting, and pagination.

## Default OSS behavior

The default no-op authorizer will return `ScopeAll`.

This default keeps existing OSS behavior when no scoped authorizer is installed.

Services outside the initial scope will continue to accept their current authorizer type.

Kagent will not define policy resources, subjects, grants, or access levels.

## Validation

Tests must cover `ScopeAll`, `ScopeNone`, and `ScopeAnyOf`.

Tests must verify OR clauses, AND predicates, and `ScopeIn`.

Tests must verify trusted attributes for each single-resource operation.

Tests must prove that list filtering occurs before sorting and response construction.

Tests must prove that each protected list requests the correct resource type and filters unauthorized entries.

Tests must verify fail-closed behavior for invalid scopes.

## Implementation checklist

- [x] Add `name` to the shared authorization attributes.
- [x] Add one Kubernetes scope matcher for `namespace` and `name`.
- [x] Require `CollectionAuthorizer` for `AgentTemplate` collection operations.
- [x] Require `CollectionAuthorizer` for `Harness` collection operations.
- [x] Require `CollectionAuthorizer` for `ModelConfig` collection operations.
- [x] Populate trusted attributes for reads and writes.
- [x] Filter each protected list before response construction.
- [x] Add focused service and matcher tests.
- [x] Add action capabilities to the protected RPC responses.

## Alternatives

Per-item checks after pagination produce incomplete pages and incorrect totals.

Separate allowed-name and allowed-namespace lists can lose required AND relationships.

Raw query fragments couple an `Authorizer` to storage and create an unsafe trust boundary.

An absent-attribute predicate adds contract and validation complexity without matching any initial protected resource.
