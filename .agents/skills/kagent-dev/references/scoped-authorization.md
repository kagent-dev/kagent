# Scoped authorization implementation guide

The source contract is [design/EP-1270-scoped-authorization.md](../../../../design/EP-1270-scoped-authorization.md). This guide describes how to apply it to Kubernetes-backed configuration resources.

## Mental model

An authorizer decides which resources a principal may access. It returns an `AuthorizationScope`; it never returns SQL, Kubernetes selectors, policy objects, or backend field names.

- `ALL` permits the complete collection and has no clauses.
- `NONE` permits no resources and has no clauses.
- `ANY_OF` is an OR of non-empty clauses.
- Each clause is an AND of non-empty predicates.
- `IN` matches one of its non-empty values.

For example, this scope:

```text
(namespace IN [team-a] AND name IN [agent-a, agent-b])
OR namespace IN [shared]
```

is represented as two `AnyOf` clauses. The first contains both predicates in `All`; the second contains one predicate.

The shared scope types and attribute names live in [`go/api/authorization`](../../../../go/api/authorization/scope.go). Services use those public types directly.

## Resource identities

Current object checks use `auth.Resource{Type, Namespace, Name}`.
Validate the requested reference and authorize its exact namespace/name before reading protected content or writing storage.
This ordering prevents a denied caller from distinguishing a missing object from an existing one.
Creates validate and normalize the proposed object identity before checking authorization.
Do not add caller-controlled attributes beyond this identity without defining their trusted source and validation.

## Collection flow

Implement a protected collection request in this order:

1. Validate caller-supplied filters.
2. Call the required `CollectionAuthorizer.Scope` with `VerbList` and the protected resource type.
3. Compile the scope with [`kubeauth.CompileScope`](../../../../go/core/internal/service/kubeauth/scope.go).
4. List a safe, complete Kubernetes resource set.
5. Apply the matcher to object metadata.
6. Sort and build the response from authorized objects only.

The matcher accepts only `namespace` and `name`. It rejects malformed scopes, unsupported attributes or operators, and empty `IN` values. Treat matcher and authorizer errors as authorization failures; never convert them to `ScopeAll`.

Current list operations do not paginate, so an in-memory filter is complete. If pagination is added, authorization must happen before totals, sorting, and page slicing.

Pass scopes as ordinary function arguments. Do not place policy decisions in request context.

## Service integration

- `Agent`, `AgentTemplate`, `Harness`, and `SandboxTemplate` use `kubecrud.NewService` with a required `CollectionAuthorizer`.
- `ModelConfig` and `ModelProviderConfig` use separate typed CRUD services and resource types. Configured-provider reads and refresh are authorized independently from ModelConfig operations.
- Prompt libraries, tool discovery, and WorkerPool summary rows use `kubeauth.CollectionMatcher` for custom collection projection.
- Flat tool discovery must filter through the same exact owning-server identities as server discovery.
- `GetSubstrateSummary` requires `Check(VerbGet, Resource{Type: "SystemSummary"})` before any resource reads; denial fails the whole request with permission denied. WorkerPool rows then use their own `VerbList` scope.
- The summary capability grants endpoint access only. ATE inventory uses independent `VerbList` scopes for `SubstrateActor`, `SubstrateActorTemplate` (both `atespace`/`name`), and `SubstrateWorker` (`namespace`/`name`). Dedicated actor and worker endpoints require their respective list scopes, without a `Substrate` or `SystemSummary` capability. Use `inventoryauth.CollectionMatcher` and intersect scopes with watched and requested locations before pagination or counts. See the source contract for cursor and embedded-placement rules.
- Static provider definitions and supported tool-server types expose no configured object and remain available after authentication.

The default OSS no-op authorizer implements `CollectionAuthorizer` and returns `ScopeAll`, preserving current OSS visibility.

## Required tests

Test generic scope behavior once in the Kubernetes matcher:

- `ALL`, `NONE`, and `ANY_OF`;
- OR clauses and AND predicates;
- `IN`;
- every invalid kind, attribute, operator, clause shape, and value shape.

Each protected service then needs focused tests proving:

- it requests `VerbList` for the correct resource type;
- denied objects are absent before sorting or response construction;
- single-resource checks receive trusted `namespace` and `name` attributes;
- updates check the stored object;
- malformed scopes fail closed.
