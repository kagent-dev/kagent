# Scoped authorization implementation guide

The source contract is [design/scoped-authorization.md](../../../../design/scoped-authorization.md). This guide describes how to apply it to Kubernetes-backed configuration resources.

## Mental model

An authorizer decides which resources a principal may access. It returns an `AuthorizationScope`; it never returns SQL, Kubernetes selectors, policy objects, or backend field names.

- `ALL` permits the complete collection and has no clauses.
- `NONE` permits no resources and has no clauses.
- `ANY_OF` is an OR of non-empty clauses.
- Each clause is an AND of non-empty predicates.
- `IN` matches one of its non-empty values.
- `MISSING` matches an absent attribute and has no values.

For example, this scope:

```text
(namespace IN [team-a] AND name IN [agent-a, agent-b])
OR namespace IN [shared]
```

is represented as two `AnyOf` clauses. The first contains both predicates in `All`; the second contains one predicate.

The shared scope types and attribute names live in [`go/api/authorization`](../../../../go/api/authorization/scope.go). The core authorization package aliases them for service use.

## Trusted resource attributes

Use [`kubeauth.Resource`](../../../../go/core/internal/service/kubeauth/scope.go) to construct authorization input from a Kubernetes object. It supplies `namespace` and `name` from object metadata and preserves the existing `namespace/name` value in `Resource.Name`.

For a single-resource operation:

- Read and delete: load the stored object, then authorize it.
- Create: validate and normalize the proposed object, then authorize it before checking or writing storage.
- Update: authorize the stored object and the validated proposed object before any write.

Do not treat a request reference as stored resource data. Request references are suitable for loading an object, not for constructing its trusted attributes.

## Collection flow

Implement a protected collection request in this order:

1. Validate caller-supplied filters.
2. Call the required `CollectionAuthorizer.Scope` with `VerbList` and the protected resource type.
3. Compile the scope with [`kubeauth.ScopeMatcher`](../../../../go/core/internal/service/kubeauth/scope.go).
4. List a safe, complete Kubernetes resource set.
5. Apply the matcher to object metadata.
6. Sort and build the response from authorized objects only.

The matcher accepts only `namespace` and `name`. It rejects malformed scopes, unsupported attributes or operators, and empty `IN` values. Treat matcher and authorizer errors as authorization failures; never convert them to `ScopeAll`.

Current list operations do not paginate, so an in-memory filter is complete. If pagination is added, authorization must happen before totals, sorting, and page slicing.

Pass scopes as ordinary function arguments. Do not place policy decisions in request context.

## Service integration

- `AgentTemplate` and `Harness` use `kubecrud.NewScopedService`. Their gRPC servers accept the scoped service type, so they cannot be wired with the unscoped constructor accidentally.
- `ModelConfig` requests require a `CollectionAuthorizer`; `Service.List` filters the returned Kubernetes list before transport conversion.
- `ListConfiguredProviders` reads the separate `ModelProviderConfig` resource and remains outside this scope.
- Legacy `AgentHarness` and `SandboxAgent` operations remain outside this scope.

The default OSS no-op authorizer implements `CollectionAuthorizer` and returns `ScopeAll`, preserving current OSS visibility.

## Required tests

Test generic scope behavior once in the Kubernetes matcher:

- `ALL`, `NONE`, and `ANY_OF`;
- OR clauses and AND predicates;
- `IN` and `MISSING`;
- every invalid kind, attribute, operator, clause shape, and value shape.

Each protected service then needs focused tests proving:

- it requests `VerbList` for the correct resource type;
- denied objects are absent before sorting or response construction;
- single-resource checks receive trusted `namespace` and `name` attributes;
- updates check both stored and proposed objects;
- malformed scopes fail closed.
