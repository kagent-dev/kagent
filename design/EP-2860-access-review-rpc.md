# EP-2860: Batched access review for catalog actions

> **Discussion draft:** keep this document in the draft PR while the API is being
> reviewed, then remove it before merge.

* OSS issue: [kagent-dev/kagent#2860](https://github.com/kagent-dev/kagent/issues/2860)
* Enterprise context: [solo-io/enterprise-kagent#95](https://github.com/solo-io/enterprise-kagent/issues/95)

## Summary

Add an authenticated `AuthorizationService.CheckAccess` RPC that returns an
advisory permission matrix for catalog actions. One request reviews several
namespaced targets of one resource type against several verbs.

This replaces the UX purpose of the earlier `canCreate`, `canUpdate`, and
`canDelete` response fields without embedding authorization state in catalog
resources. The enterprise UI can decide whether to hide or disable an action,
while every catalog operation continues to authorize the real request.

## Goals

- Let a UI decide whether to present create, get, update, and delete actions.
- Preserve the earlier `canCreate` behavior before the user starts filling in a
  form.
- Review a whole page of resource actions in one browser request.
- Reuse the authenticated principal, resource names, verbs, and authorization
  scopes already used by catalog services.
- Keep the default OSS authorizer behavior unchanged.
- Keep access-review results advisory and independent from mutation enforcement.

## Non-goals

- Add capability fields to catalog list or item responses.
- Return roles, policies, claims, denial reasons, catalog keys, or raw scopes.
- Review `LIST`; partial collection visibility is not representable by one
  Boolean.
- Read Kubernetes resources as part of a review.
- Cache decisions on the server or turn a successful review into a grant.
- Add access-aware behavior to the OSS UI. The consumer is the enterprise UI.

## Proposed API

```proto
service AuthorizationService {
  rpc CheckAccess(CheckAccessRequest) returns (CheckAccessResponse);
}

enum AuthorizationResourceType {
  AUTHORIZATION_RESOURCE_TYPE_UNSPECIFIED = 0;
  AUTHORIZATION_RESOURCE_TYPE_AGENT_TEMPLATE = 1;
  AUTHORIZATION_RESOURCE_TYPE_HARNESS = 2;
  AUTHORIZATION_RESOURCE_TYPE_MODEL_CONFIG = 3;
}

enum AuthorizationVerb {
  AUTHORIZATION_VERB_UNSPECIFIED = 0;
  AUTHORIZATION_VERB_GET = 1;
  AUTHORIZATION_VERB_CREATE = 2;
  AUTHORIZATION_VERB_UPDATE = 3;
  AUTHORIZATION_VERB_DELETE = 4;
}

message AccessTarget {
  string namespace = 1;
  optional string name = 2;
}

message CheckAccessRequest {
  AuthorizationResourceType resource_type = 1;
  repeated AuthorizationVerb verbs = 2;
  repeated AccessTarget targets = 3;
}

message CheckAccessResponse {
  repeated ResourceAccess results = 1;
}

message ResourceAccess {
  AccessTarget target = 1;
  repeated AuthorizationVerb allowed_verbs = 2;
}
```

One request is homogeneous by resource type. That matches the common UI surfaces
(a template list, a model list, or a harness list) and lets the server obtain one
authorization scope per requested verb. A screen containing multiple catalog
resource types can issue at most three requests in parallel.

`results` has the same order and cardinality as `targets`. Echoing the target also
makes the response self-describing and avoids string-encoding a namespaced name as
a protobuf map key.

### Validation

Declare request-intrinsic validation in the source proto with `buf.validate`:

- `resource_type` must be a defined, non-zero enum value.
- `verbs` must contain between one and four unique, defined, non-zero values.
- `targets` must contain between one and 100 entries.
- Every namespace is a required Kubernetes DNS label.
- A present name is a non-empty Kubernetes DNS subdomain.
- Unsupported resource/verb combinations are rejected. The initial matrix is:

  | Resource type | Verbs |
  | --- | --- |
  | `AgentTemplate` | `GET`, `CREATE`, `UPDATE`, `DELETE` |
  | `Harness` | `CREATE`, `DELETE` |
  | `ModelConfig` | `GET`, `CREATE`, `UPDATE`, `DELETE` |

The limit bounds one request to at most 400 Boolean decisions. It is large enough
for the current 25-row UI pages and prevents an access-review call from becoming
an unbounded policy-evaluation endpoint.

## Semantics

### Named targets

For a target with `name`, a verb is returned in `allowed_verbs` when the caller's
action scope matches the exact `(resource type, namespace, name)` identity.

The review does not load the named resource. This avoids an existence side channel
and keeps the result advisory: the subsequent get, update, or delete loads or
validates its real input and authorizes it again.

### Namespace targets and `canCreate`

For a target without `name`, a verb is allowed when at least one valid resource
name in that namespace can satisfy its action scope:

- `ALL` allows.
- `NONE` denies.
- `ANY_OF` allows when at least one clause accepts the namespace and contains a
  satisfiable name after all name predicates in that clause are applied.

This is the direct replacement for the earlier collection-level `canCreate`:
"some proposed resource in this namespace could be allowed." It does not
authorize the object eventually submitted.

The same existential meaning can apply consistently to every verb, although the
first concrete caller for a nameless target is `CREATE`. Restricting nameless
targets to `CREATE` is an API-review option if broader queries are considered
unnecessary policy disclosure.

Namespace remains required. A global create button can batch the namespaces the
UI already lists and show when any result allows `CREATE`. This also lets the form
disable unauthorized namespace choices. An implicit "any namespace" query is not
needed for the current UI.

### Denials and failures

- A policy denial is a successful response in which the verb is absent from
  `allowed_verbs`.
- Missing authentication returns `Unauthenticated`.
- Invalid input returns `InvalidArgument` through Protovalidate.
- Failure to obtain an authorization scope returns `Unavailable`.
- A malformed scope returned by an authorizer returns `Internal`.

The first version fails the whole RPC if any requested action scope cannot be
evaluated. Per-cell errors add a second error model for little UX value: the UI
must already treat the entire review as advisory and keep handling
`PermissionDenied` from the real operation.

## Evaluation and performance

For each requested verb, the server asks `CollectionAuthorizer.Scope` once for
the authenticated principal and resource type, compiles the result with the
existing Kubernetes authorization matcher, and applies it to every target:

```text
for verb in request.verbs:
    matcher = CompileScope(authorizer.Scope(principal, verb, resourceType))
    for target in request.targets:
        allowed = target.name is present
            ? matcher.Matches(namespace, name)
            : matcher.MatchesAnyName(namespace)
```

The authorizer must derive `Check` and `Scope` from the same policy evaluation so
an exact target produces the same answer through either form. This is also the
invariant required by the earlier capability-field design in enterprise issue
#95, which calculated item and collection capabilities from the corresponding
action scope.

For a page of 100 resources showing update and delete actions:

| Shape | Browser requests | Authorizer scope evaluations | Local matches |
| --- | ---: | ---: | ---: |
| One RPC per resource and verb | 200 | up to 200 | 0 |
| Batched matrix | 1 | 2 | 200 |

There is no server cache. A review may become stale immediately, so caching it as
a grant would be incorrect. A browser data cache may deduplicate identical
in-flight reviews, but catalog operations remain authoritative.

## UI flows

### Collection-level create action

Once the UI knows the candidate namespaces, it sends one nameless target per
namespace with `CREATE`:

```json
{
  "resourceType": "AGENT_TEMPLATE",
  "verbs": ["CREATE"],
  "targets": [
    {"namespace": "kagent"},
    {"namespace": "team-a"}
  ]
}
```

The enterprise UI can show the global create button if any target allows
`CREATE`, then allow only those namespaces in the form. The review can run in
parallel with the catalog and namespace reads; capability fields also were not
available until their containing collection response arrived.

### Per-item actions

After a list loads, the UI sends its visible rows as named targets and requests
the verbs rendered on that page. A 25-row template page therefore makes one
review request rather than 50 update/delete requests.

### Detail actions

A detail page sends one named target with `UPDATE` and `DELETE`. The matrix API
handles the single-target case, so a second singular RPC is unnecessary.

### Loading, errors, and staleness

The enterprise UI owns whether a denied action is hidden or disabled. While the
review is loading it can hold the action area or render a stable placeholder to
avoid flashing unauthorized controls.

A review transport failure is not a policy denial. The UI should preserve its
existing fallback behavior and let the authoritative operation return
`PermissionDenied`; otherwise a transient advisory failure becomes an accidental
availability failure.

## Backend boundaries

- The protobuf adapter maps the closed enums to the canonical `auth.Verb` and
  catalog resource-type values.
- A transport-independent access-review service derives the principal from the
  authenticated context and evaluates action scopes.
- `kubeauth.Matcher` owns exact and existential target matching.
- `AuthorizationService.CheckAccess` has `AccessRead` method policy so the caller
  is authenticated before the requested catalog verbs are evaluated.
- The service does not use a Kubernetes client or database.
- Generated Go and TypeScript clients are committed from the source proto.

The OSS UI does not call the RPC. The generated TypeScript contract is consumed
by a follow-up enterprise UI change.

## Security properties

- Results apply only to the authenticated caller.
- Named checks do not reveal whether a resource exists.
- No policy representation or denial explanation crosses the API boundary.
- Request limits bound policy work.
- A successful review never bypasses authorization on a later operation.
- The default `NoopAuthorizer` returns `ALL`, preserving the OSS experience.

## Testing

- Protovalidate rejects invalid enums, duplicates, empty lists, oversized target
  sets, invalid namespaces and names, and unsupported resource/verb pairs.
- Scope matching covers `ALL`, `NONE`, namespace/name conjunctions, OR clauses,
  repeated name predicates, invalid candidate names, and nameless targets.
- Service tests prove one scope lookup per verb rather than per target.
- Service tests prove results preserve target order and contain only allowed
  requested verbs.
- gRPC tests prove authentication, enum mapping, registration, and default OSS
  allow behavior.
- Mutation tests continue to prove that a prior allowed review does not bypass a
  later denial.
- Enterprise UI tests cover the global create action, namespace choices,
  per-item actions, loading, review failure fallback, and mutation-time denial.

## Alternatives

### Capability fields on catalog responses

They avoid the extra review request, but couple catalog schemas and every catalog
handler to UI actions, become stale with unrelated resource data, and cannot be
refreshed independently. This was rejected by OSS issue #2710 and is the reason
for the dedicated review API.

### A repeated list of fully independent checks

This removes browser round trips but repeats resource type and verb data for each
cell and encourages one authorizer evaluation per cell. Grouping one resource
type, several verbs, and several targets expresses the matrix directly and makes
scope reuse natural.

### Return authorization scopes to the browser

This would minimize server work but expose policy representation and require the
UI to duplicate the scope matcher. It also makes policy-format compatibility a
public API concern.

### Separate singular and batch RPCs

The matrix handles one target and one verb without special cases. A second RPC
would duplicate validation, mapping, tests, and client code.

## Questions for review

1. Should a nameless target retain uniform existential semantics for every verb,
   or be valid only for `CREATE`?
2. Is 100 the right initial target limit for the enterprise UI's largest rendered
   page?
3. Should `ResourceAccess` echo each target, or rely only on request/response
   ordering for a smaller response?
4. When an update operation requires more than `UPDATE` authorization (for
   example, a separate `GET` prerequisite), should the `UPDATE` matrix cell be the
   conjunction of all operation prerequisites?
5. Does the enterprise authorizer guarantee that action scopes and exact checks
   are equivalent for namespace/name attributes? If not, the wire API can remain
   batched while the initial server implementation loops over exact `Check` calls.
