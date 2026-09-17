# EP-2860: Batched access review for catalog actions

> **Discussion draft:** remove this document before the PR merges.

- OSS issue: [kagent-dev/kagent#2860](https://github.com/kagent-dev/kagent/issues/2860)
- Enterprise context: [solo-io/enterprise-kagent#95](https://github.com/solo-io/enterprise-kagent/issues/95)

## Summary

Add an authenticated `AuthorizationService.CheckAccess` RPC that returns an
advisory permission matrix. One request checks several targets of one catalog
resource type against several verbs.

This gives the enterprise UI the same early UX signals as the former
`canCreate`, `canUpdate`, and `canDelete` fields without coupling authorization
state to catalog resources. Real operations continue to authorize every request.

## Scope

The RPC should:

- support create-button, namespace-picker, and per-item action UX;
- review a page of resources in one browser request;
- reuse the existing resource types, verbs, and authorization scopes; and
- preserve the default OSS authorizer behavior.

It will not return roles, policies, denial reasons, or raw scopes; review `LIST`;
read Kubernetes resources; add server-side caching; or change the OSS UI.

## API

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

One resource type per request matches current UI surfaces and lets the server
evaluate each requested verb's scope once. Results preserve target order and echo
the target so callers need no encoded map key.

### Validation

Declare validation in the proto with `buf.validate`:

- require defined, non-zero enums;
- require 1–4 unique verbs and 1–100 targets;
- require a Kubernetes DNS-label namespace;
- when present, require a non-empty DNS-subdomain name; and
- reject unsupported resource/verb combinations.

Initial supported verbs:

| Resource | Verbs |
| --- | --- |
| `AgentTemplate` | `GET`, `CREATE`, `UPDATE`, `DELETE` |
| `Harness` | `CREATE`, `DELETE` |
| `ModelConfig` | `GET`, `CREATE`, `UPDATE`, `DELETE` |

The limits bound a request to 400 decisions while covering current 25-row pages.

## Semantics

### Named target

`{namespace, name}` checks the exact resource identity. The review does not load
the resource, avoiding an existence side channel. The later operation loads its
real input and authorizes again.

### Namespace target

`{namespace}` asks whether at least one valid resource name in that namespace
could satisfy the action scope. For `CREATE`, this replaces collection-level
`canCreate`; it does not authorize the object eventually submitted.

Namespace is always required. To decide a global Create button, the UI sends all
candidate namespaces as targets in one request and shows the button if any allow
`CREATE`. It can then hide or disable denied namespaces in the form.

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

### Denials and failures

- A denied verb is absent from `allowed_verbs`.
- Missing authentication returns `Unauthenticated`.
- Invalid input returns `InvalidArgument`.
- Scope lookup failure returns `Unavailable`.
- A malformed authorizer scope returns `Internal`.

The first version fails the whole RPC if any scope cannot be evaluated. Per-cell
errors add little value because the review is advisory and real operations remain
authoritative.

## Evaluation and performance

For each requested verb, call `CollectionAuthorizer.Scope` once, compile the
existing matcher, and apply it to every target:

```text
for verb in request.verbs:
    matcher = CompileScope(authorizer.Scope(principal, verb, resourceType))
    for target in request.targets:
        allowed = target.name is present
            ? matcher.Matches(namespace, name)
            : matcher.MatchesAnyName(namespace)
```

`Check` and `Scope` must derive from the same policy evaluation so exact-target
answers agree.

For 100 rows showing update and delete actions:

| Shape | Browser requests | Scope evaluations | Local matches |
| --- | ---: | ---: | ---: |
| One RPC per resource and verb | 200 | up to 200 | 0 |
| Batched matrix | 1 | 2 | 200 |

There is no server cache. The enterprise UI may use SWR to deduplicate or briefly
cache advisory responses, but a cached result never bypasses operation-time
authorization.

## UI behavior

- **Collection:** check `CREATE` for candidate namespaces while loading the page;
  show Create if any namespace allows it.
- **Create form:** show or enable only allowed namespaces.
- **List:** check visible named rows for the actions rendered on that page.
- **Detail:** send one named target with the required actions.
- **Loading:** avoid flashing controls before the review completes.
- **Review failure:** do not treat transport failure as policy denial; preserve a
  path to the authoritative operation.

The OSS UI will not call the RPC. Generated TypeScript is consumed by the
enterprise UI.

## Backend and security

- The protobuf adapter maps closed enums to canonical `auth.Verb` and catalog
  resource types.
- A transport-independent service obtains the authenticated principal and
  evaluates scopes.
- `kubeauth.Matcher` owns exact and existential matching.
- The RPC requires `AccessRead` method policy.
- It uses no Kubernetes client or database.
- The default `NoopAuthorizer` returns `ALL`.
- Responses reveal only Boolean action results, not policy structure or resource
  existence.

## Tests

- Proto validation: enums, duplicate verbs, limits, names, namespaces, and
  supported combinations.
- Matcher: `ALL`, `NONE`, exact targets, namespace/name predicates, OR clauses,
  and nameless targets.
- Service: one scope lookup per verb, stable result order, and allowed verbs.
- gRPC: authentication, enum mapping, registration, and default allow behavior.
- Enterprise UI: Create visibility, namespace choices, per-item actions, loading,
  advisory failure, and mutation-time denial.

## Alternatives rejected

- **Capability fields:** couple authorization to catalog schemas and cannot be
  refreshed independently.
- **Independent check list:** repeats resource and verb data and encourages one
  policy evaluation per cell.
- **Scopes in the browser:** expose policy representation and duplicate matcher
  semantics.
- **Separate singular RPC:** duplicates an API already covered by a one-cell
  matrix.

## Questions for review

1. Should nameless targets work for every verb or only `CREATE`?
2. Is 100 the right initial target limit?
3. Should results echo targets or rely only on ordering?
4. If an operation has multiple authorization prerequisites, should its matrix
   cell require all of them?
5. Does the enterprise authorizer guarantee equivalent `Scope` and exact `Check`
   decisions? If not, the wire API can remain batched while the initial server
   loops over `Check` calls.
