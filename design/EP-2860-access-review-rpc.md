# EP-2860: Access review for catalog actions

* Issue: [#2860](https://github.com/kagent-dev/kagent/issues/2860)

## Summary

Add an authenticated, read-only `AuthorizationService.CheckAccess` RPC that lets a client ask whether the current caller may perform a catalog action. The answer is advisory: the corresponding catalog RPC remains the authoritative enforcement point and must authorize again when it runs.

This extends [EP-1270](EP-1270-scoped-authorization.md) without putting capability fields back into `AgentTemplate`, `Harness`, or `ModelConfig` responses.

## Motivation

EP-1270 deliberately removed `can_create`, `can_update`, and `can_delete` fields from catalog responses. Embedded hints duplicate policy decisions, become stale with the resource that carried them, and couple resource schemas to presentation behavior.

The UI still needs a way to explain unavailable actions before a caller submits a write. A dedicated review request keeps that concern separate from catalog data and makes its advisory lifetime explicit.

### Goals

- Review `GET`, `CREATE`, `UPDATE`, and `DELETE` for the protected catalog resources in EP-1270.
- Use the authenticated request principal and the same canonical resource types, verbs, and attributes as the corresponding operation.
- Support a namespace-level create review before a proposed name is known.
- Keep every real catalog operation authoritative and unchanged.
- Make the review available to browser clients through the generated TypeScript API.
- Fail closed without revealing whether a named resource exists.

### Non-goals

- Return policy rules, roles, claims, scopes, or denial explanations.
- Review `LIST`; partial collection access is not representable by one Boolean.
- Add capability fields to catalog resources or list responses.
- Cache an authorization result on the server or turn it into a grant.
- Expand authorization to resources outside `AgentTemplate`, `Harness`, and `ModelConfig`.
- Batch reviews in the first version. Add batching only if measured UI request volume requires it.

## API

Add `proto/kagent/api/v1alpha1/authorization.proto`:

```proto
service AuthorizationService {
  rpc CheckAccess(CheckAccessRequest) returns (CheckAccessResponse);
}

enum AccessReviewResourceType {
  ACCESS_REVIEW_RESOURCE_TYPE_UNSPECIFIED = 0;
  ACCESS_REVIEW_RESOURCE_TYPE_AGENT_TEMPLATE = 1;
  ACCESS_REVIEW_RESOURCE_TYPE_HARNESS = 2;
  ACCESS_REVIEW_RESOURCE_TYPE_MODEL_CONFIG = 3;
}

enum AccessReviewVerb {
  ACCESS_REVIEW_VERB_UNSPECIFIED = 0;
  ACCESS_REVIEW_VERB_GET = 1;
  ACCESS_REVIEW_VERB_CREATE = 2;
  ACCESS_REVIEW_VERB_UPDATE = 3;
  ACCESS_REVIEW_VERB_DELETE = 4;
}

message CheckAccessRequest {
  AccessReviewResourceType resource_type = 1;
  AccessReviewVerb verb = 2;
  string namespace = 3;
  optional string name = 4;
}

message CheckAccessResponse {
  bool allowed = 1;
}
```

The source proto owns request validation:

- `resource_type` and `verb` must be defined, non-zero enum values.
- `namespace` is a required DNS-1123 subdomain.
- A present `name` is a non-empty DNS-1123 subdomain. Absence is distinct from an empty string.
- `name` may be absent only for `CREATE`. Reads, updates, and deletes address a concrete resource.
- The supported operation matrix is:

  | Resource | Verbs |
  | --- | --- |
  | `AgentTemplate` | `GET`, `CREATE`, `UPDATE`, `DELETE` |
  | `Harness` | `CREATE`, `DELETE` |
  | `ModelConfig` | `GET`, `CREATE`, `UPDATE`, `DELETE` |

Use standard `buf.validate` rules first and message CEL only for the name/verb and resource/verb combinations.

`optional string name` is intentional. Treating an empty string as “any name” would make a malformed named request silently broader.

## Review semantics

### Named review

For a present `name`, construct the same `auth.Resource` identity the catalog operation uses and evaluate the operation's authorization checks without reading Kubernetes.

- `GET`, `CREATE`, and `DELETE` evaluate their matching `auth.Verb`.
- `UPDATE` evaluates every authorization prerequisite of the real update flow. After #2859, `AgentTemplate` and `ModelConfig` updates require both the read and update checks, so the review is allowed only when both checks allow the same named resource.
- An authorizer rejection returns `allowed: false`; it is not a failed RPC. The existing `Authorizer.Check` contract represents every denial as an error and has no separate backend-failure category, so the review treats any `Check` error the same way the catalog services do: denied.

This is an advisory check against the proposed reference, not trusted evidence for a mutation. The later catalog RPC still validates or loads the real resource and authorizes it independently.

### Namespace-level create review

When `name` is absent, request the `CREATE` scope for the resource type and ask whether at least one valid name in the requested namespace can satisfy it:

- `ALL` allows.
- `NONE` denies.
- `ANY_OF` allows when at least one clause accepts the namespace and has at least one satisfiable name after all `name IN (...)` predicates in that clause are intersected.

Add this as a semantic operation on the existing compiled `kubeauth.Matcher`; do not duplicate scope parsing in the access-review service. Invalid scope output remains an internal failure, and an authorizer backend failure remains unavailable.

### Error behavior

- Missing authenticated session: `Unauthenticated`.
- Invalid request or unsupported resource/verb combination: `InvalidArgument` through Protovalidate.
- Policy rejection: successful response with `allowed: false`.
- Failure to obtain a scope: `Unavailable`.
- Malformed scope returned by an authorizer: `Internal`.

The API intentionally returns no denial reason. Exposing backend-specific policy explanations would couple the public contract to an authorizer and can leak policy details.

## Backend implementation

1. Define canonical catalog resource-type constants beside `auth.Resource` and replace the current repeated string literals in model and kubecrud wiring.
2. Add a transport-independent `go/core/internal/service/accessreview` service over `auth.CollectionAuthorizer`.
3. Add the thin gRPC adapter that maps protobuf enums to the existing auth verbs and canonical resource types.
4. Register the service in `grpcserver.Config` and `app.Run`.
5. Add `AuthorizationService.CheckAccess` to `DefaultMethodPolicies` as `AccessRead`, so authentication runs before the handler while the requested catalog verb remains data evaluated by the service.
6. Regenerate Go and TypeScript protobuf outputs from the source proto.
7. Update EP-1270 and the scoped-authorization development guide to distinguish rejected embedded hints from the explicit advisory review API.

No Kubernetes client, database, new dependency, or server-side cache is needed.

## UI integration

Add the first bundled UI caller in the same PR as the RPC. Keep the API/core and UI work in separate commits so the generated contract and policy semantics can still be reviewed before the presentation changes.

1. Add one stable `authorization.checkAccess` operation and an SWR-backed hook keyed by resource type, verb, namespace, and optional name.
2. Use exact-name reviews for edit, save, and delete controls. Use the namespace-level create review only where the namespace is known before the name.
3. Disable rather than hide unavailable actions and provide an accessible explanation. Direct navigation to a form must also review the submit action.
4. Do not treat a review error as a denial. Leave the action available and let the authoritative operation report its result; optionally show the review failure as advisory UI state.
5. Continue handling `PermissionDenied` from every mutation because a prior allowed result can become stale immediately.
6. Avoid eager checks for every off-screen table row. Review the actions rendered on the current page or when an action surface opens; add a batch RPC only if this is still measurably expensive.

The mock transport must model allowed, denied, and failed reviews rather than defaulting every check to allowed.

## Delivery plan

Ship one end-to-end PR so the new RPC has a real caller in the same change:

1. Proto contract, validation, generated Go/TypeScript artifacts, canonical resource mapping, access-review service, namespace-level create-scope matching, gRPC/app wiring, and backend tests.
2. Stable UI operation, hook, mock implementation, access-aware controls, and UI tests.
3. Documentation updates and full focused validation.

Before implementation, rebase this branch onto `upstream/main` after #2859 merges so `UPDATE` reviews mirror the final read/write authorization sequence without stacking #2860 on the active PR.

## Test plan

### Semantic unit tests

- Named reviews pass the authenticated principal, canonical type, namespace, name, and expected verb sequence.
- `UPDATE` requires every check used by the corresponding update operation.
- Namespace-level create handles `ALL`, `NONE`, namespace-only clauses, name-only clauses, namespace/name conjunctions, OR clauses, and intersecting repeated name predicates.
- Unsatisfiable or malformed scopes fail closed.
- A missing session is unauthenticated and a policy rejection is `allowed: false`.

### gRPC and generation checks

- Protovalidate rejects unspecified enums, bad DNS names, a missing name for non-create verbs, and unsupported resource/verb pairs before the handler runs.
- The method policy requires authentication.
- An in-process gRPC server with a scoped test authorizer proves named denial, namespace-level allow/deny, and that a successful review does not bypass a later denied mutation.
- `buf lint`, `buf generate`, and the repository generated-output check pass.

### End-to-end

- The default OSS authorizer returns `allowed: true` for every supported review.
- UI tests cover the non-authoritative behavior described above.

Run at minimum:

```bash
make proto-lint
make proto-check
make -C go test
make -C go lint
(cd ui && yarn typecheck && yarn test && yarn lint)
```

Run the focused Go and UI tests first, then the relevant Playwright and Kind E2E cases before the PR is ready to merge.

## Alternatives

- **Capability fields on catalog responses:** rejected by EP-1270 because they duplicate policy decisions and age with unrelated resource data.
- **Calling the mutation and handling `PermissionDenied`:** remains mandatory as enforcement, but is a worse first interaction when the UI can cheaply ask in advance.
- **Returning the full authorization scope:** rejected because it exposes policy representation and makes every client implement the matcher.
- **String resource types and verbs:** rejected because the supported set is closed and protobuf enums let Protovalidate reject unknown values before service code.
- **Omitted name for every verb:** deferred. Current UI reads, updates, and deletes concrete resources; only creation has a real action before a name exists. Broader existential semantics can be added with a demonstrated caller.
- **Batch review in v1:** deferred until request volume is measured.
