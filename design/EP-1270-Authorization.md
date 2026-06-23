# EP-1270: Authorization (Access Control)

* Issue: [#1270](https://github.com/kagent-dev/kagent/issues/1270)
* Status: `provisional`

## Background

KAgent can now authenticate users. EP-476 / [#1293](https://github.com/kagent-dev/kagent/pull/1293)
added an OIDC proxy authentication mode (`trusted-proxy`) where an upstream
oauth2-proxy validates the OIDC flow and injects a JWT, and the controller
extracts the caller's identity and claims from it.

What KAgent still lacks is **authorization**. The controller ships with
`NoopAuthorizer` ([`go/core/internal/httpserver/auth/authz.go`](../go/core/internal/httpserver/auth/authz.go)),
whose `Check` always returns `nil`. Concretely: once a user is authenticated,
they can list, invoke, edit and delete **every** Agent, ModelConfig and
ToolServer, across **every** namespace. Enabling OIDC today gives operators
authentication but a false sense of access control — there is none.

This is a security gap, not only a missing feature: any shared or multi-tenant
deployment is effectively wide-open to every authenticated principal.

The good news is that the enforcement plumbing already exists. The
`auth.Authorizer` interface is wired into every HTTP handler via the `Check(...)`
helper (~25 call sites in `go/core/internal/httpserver/handlers`), and
`ProxyAuthenticator` already populates `Principal.Claims` with the full JWT
payload. The missing piece is a real `Authorizer` implementation and a way for
operators to express policy.

This EP proposes that implementation. It is the fine-grained-authorization
follow-on that EP-476 explicitly deferred ("detailed RBAC policies come in
future iterations").

**Sponsors**: TBD (seeking maintainer sponsor)

**Prior art (both stalled on inactivity, not rejection):**
- [#1766](https://github.com/kagent-dev/kagent/pull/1766) — per-agent
  `kagent.dev/allowed-groups` annotation, `GroupAuthorizer`, agent-list
  filtering, and A2A request gating.
- [#1370](https://github.com/kagent-dev/kagent/pull/1370) — pluggable external
  authorizer (OPA-style webhook) behind the `Authorizer` interface.

This proposal deliberately builds on both rather than starting over.

## Motivation

Operators running KAgent in shared environments need to control who can see and
invoke which agents (and configure model/tool resources), keyed off the identity
their existing IdP already provides. The earlier discussion on #1270 surfaced a
design tension that stalled progress:

- One camp wants an **opinionated, in-process** implementation: simple, low
  latency, no new infrastructure or single point of failure.
- The other wants a **pluggable extension point**, so the project does not get
  married to one specific RBAC engine and users can bring their own (OPA, etc.).

The core idea of this EP is that **CEL** ([Common Expression Language](https://github.com/google/cel-spec))
resolves that tension rather than picking a side. See
[Implementation Details](#implementation-details).

A second, narrower motivation: authorization must key off **arbitrary claims**,
not just `groups`. Different IdPs express authorization-relevant facts
differently — `groups`, `roles`, `realm_access.roles` (Keycloak), `email`,
domain, or custom claims. Hard-coding `groups` (as the #1766 prototype did) is
too narrow; the model should let operators match on any claim, with groups being
just one case.

### Goals

1. Provide a real, default `Authorizer` so KAgent is not open-by-default once
   authentication is enabled.
2. Allow authorization decisions to be expressed over **arbitrary JWT claims**,
   not a fixed `groups`/`roles` model.
3. Support **per-resource** access control — in particular "these principals may
   use this Agent" — authored close to the resource.
4. Keep the `auth.Authorizer` interface as a stable extension seam, so external
   engines (OPA/webhook, à la #1370) remain pluggable.
5. Make misconfiguration **visible** (surface bad policy to operators) and
   **safe** (fail closed).
6. Add no new required infrastructure or runtime single point of failure for the
   default path.
7. Preserve backward compatibility: no authorization is enforced unless the
   operator opts in.

### Non-Goals

1. **Authentication.** Covered by EP-476 / #1293. This EP assumes a `Principal`
   with claims is already established.
2. **Edge exposure / network gating.** HTTPRoute + NetworkPolicy and the
   OpenShift Route are tracked in [#2028](https://github.com/kagent-dev/kagent/issues/2028).
3. **Shipping an external authorization service.** We keep the interface
   pluggable, but do not build/operate an OPA deployment here.
4. **A built-in role/permission UI or policy editor.** Policy is configuration
   (CRD field / ConfigMap) in this iteration.
5. **Secret masking / field-level redaction** for read-only principals. Valuable
   (raised in #1270) but deferred to a follow-up.
6. **Multi-tenancy isolation guarantees** beyond what per-resource policy
   provides.

## Implementation Details

### Why CEL

CEL is proposed as the **default, in-process policy engine**, evaluated behind
the existing `auth.Authorizer` interface.

- **In-process, no new SPOF.** `github.com/google/cel-go` is already in the
  module graph (it arrives transitively with the Kubernetes apiserver
  libraries), so this adds no new runtime dependency to deploy or operate.
- **Not a hard-coded RBAC model.** A policy is an expression over the request,
  not a fixed role table. Groups become one option among many. This is precisely
  what lets us avoid committing the project to a specific RBAC engine.
- **Familiar to operators.** CEL is the same language used by Kubernetes
  ValidatingAdmissionPolicy and CRD validation rules, and by agentgateway — so
  it is consistent across the stack rather than a new bespoke DSL.
- **Safe to evaluate on the request path.** CEL is sandboxed and
  non-Turing-complete (guaranteed to terminate); compiled programs evaluate in
  microseconds.
- **The interface stays the seam.** CEL is the batteries-included default; an
  external/OPA authorizer (#1370) remains a drop-in alternative.

### The decision context exposed to a policy

A `CELAuthorizer` implements
[`auth.Authorizer`](../go/core/pkg/auth/auth.go):
`Check(ctx, principal, verb, resource) error`. The current call signature and
all handler wiring are unchanged. The following variables are exposed to every
policy expression:

| Variable | Type | Source |
|---|---|---|
| `claims` | `map(string, dyn)` | `principal.Claims` — the full raw JWT payload |
| `user` | `string` | `principal.User.ID` (the `sub`/configured claim) |
| `verb` | `string` | `get` \| `create` \| `update` \| `delete` (derived from HTTP method, as today) |
| `resource.type` | `string` | e.g. `Agent`, `ModelConfig`, `ToolServer` |
| `resource.name` | `string` | `namespace/name` |
| `resource.namespace` | `string` | parsed from the resource ref |

Example expressions:

```cel
// admin via a roles claim
has(claims.roles) && "kagent-admin" in claims.roles

// read-only for a group, on a specific resource type
verb == "get" && claims.groups.exists(g, g.startsWith("eng-"))

// attribute-based: namespace must match the caller's department claim
has(claims.dept) && resource.namespace == claims.dept

// nested Keycloak claim
has(claims.realm_access.roles) && "admin" in claims.realm_access.roles
```

Note `claims` is a dynamic map: accessing a missing key raises a CEL error
rather than returning false. Authors are expected to guard with `has(...)`, and
the evaluator treats **any evaluation error as deny** (fail closed).

### Where policy lives

Two complementary sources, both compiled and evaluated centrally in the
controller (the controller is the single Policy Enforcement *and* Decision
Point; agent pods enforce nothing):

1. **Cluster/namespace policy** — an ordered rule list in a ConfigMap
   (admin-RBAC'd). Handles coarse rules and verbs with no specific resource yet,
   e.g. "who may *create* Agents in namespace X".

2. **Per-resource policy** — a CEL expression authored on the resource itself,
   for the "who may use *this* Agent" case. Proposed on the Agent CR as either an
   annotation (`kagent.dev/access-policy`) or a typed `spec.accessPolicy` field
   (see Open Questions). Authored by whoever owns the agent, still enforced
   centrally.

<<[UNRESOLVED policy combining]>>
The combining rule needs agreement. Proposed default: **default-deny**, allow if
*either* the central policy *or* the resource's own policy permits. A per-resource
policy may only widen access to *its own* resource (it can never grant access to
other resources), so an agent owner cannot escalate beyond the resource they own.
Whether we also support explicit `deny` rules (vs. allow-only) is open.
<<[/UNRESOLVED]>>

### Enforcement is central; the controller already has the data

The `Authorizer` is constructed once in
[`cmd/controller/main.go`](../go/core/cmd/controller/main.go) and runs inside the
controller process. It can be handed the controller-runtime client via
`BootstrapConfig.Manager` (`mgr.GetClient()`), which is **cache-backed** by the
same informer the reconciler uses — so reading an Agent's policy at decision time
is an in-memory cache hit, not an API round-trip.

### Compiling policy: reconcile-driven cache, not per-request

The expensive step is compiling a CEL string into a `cel.Program` (parse +
type-check + plan), which must not happen per request. Because KAgent is a
controller, we compile from reconciliation events we already process:

- The Agent reconciler ([`agent_controller.go`](../go/core/internal/controller/agent_controller.go))
  compiles `spec.accessPolicy` on reconcile and stores the compiled program in a
  cache keyed by `NamespacedName` + `metadata.generation`.
- A bad expression is reported on `Agent.status.conditions` (the type already has
  `Conditions []metav1.Condition`), e.g. `AccessPolicyValid=False` with the
  compile error — so misconfiguration shows up on `kubectl get agent` instead of
  silently failing at request time.
- The hot path looks up the compiled program by name/generation and evaluates it.
- **Lazy fallback:** on a cache miss (e.g. a request racing the first reconcile),
  the authorizer compiles inline once and stores the result. This keeps the
  reconciler a warming/validation optimization rather than a correctness
  dependency. The central ConfigMap policy is compiled on load and recompiled on
  ConfigMap change (watch).

Optionally, the policy expression can also be validated at admission time via CRD
`x-kubernetes-validations` to reject obviously malformed input before it is
stored.

### List endpoints (the cross-cutting change)

Today the list handlers call `Check(Resource{Type: "Agent"})` with no `Name` —
a coarse "may you list at all" gate. To make the UI show only the agents a user
may see, list handlers must evaluate policy **per returned item** and filter the
response. This is the same approach #1766 took for the agent list, generalized to
the CEL evaluator and applied consistently (Agents first; ModelConfig/ToolServer
follow). This is real work independent of the policy engine and is called out so
it is not under-scoped.

### A2A path

A2A invocation must be gated too (a major risk is reaching an agent directly).
#1766 added a per-request check in the A2A handler mux; we reuse that integration
point with the CEL authorizer. Direct-to-agent calls that bypass the controller
remain out of scope here and are addressed by network gating in #2028.

### Configuration / rollout

- An auth-mode / authorizer-selection flag chooses `NoopAuthorizer` (default,
  backward compatible) vs. `CELAuthorizer`. No enforcement unless opted in.
- Helm values expose the central policy ConfigMap and the authorizer selection.
- The external authorizer (#1370) is selectable as an alternative implementation
  of the same interface.

### Test Plan

**Unit:**
- `CELAuthorizer`: expression compilation, evaluation against representative
  claim shapes (string, array, nested object, missing claim → deny), verb/resource
  mapping, fail-closed on eval error.
- Policy combining (central vs. per-resource; widen-only invariant).
- Compiled-program cache: generation-keyed invalidation, eviction on delete,
  lazy compile on miss.
- Reconciler: status condition set on valid/invalid policy.

**Integration:**
- Handler-level `Check` allow/deny across resource types and verbs.
- Agent-list filtering returns only authorized items.
- A2A gating denies unauthorized invocation.

**E2E:**
- `trusted-proxy` mode with a mock IdP issuing different claim sets; verify a
  user sees/invokes only permitted agents via the API and UI.
- Bad policy surfaces on `Agent.status` and fails closed.

## Alternatives

- **Casbin RBAC (the original #1270 proposal).** A capable RBAC engine, but it
  commits the project to one model/DSL — the exact concern raised by maintainers
  — and its role/grouping model is more than the per-resource + arbitrary-claim
  case needs. CEL expresses the same policies without the lock-in.
- **OPA / external webhook only (#1370).** More powerful (external data, full
  policy language) but adds a service to deploy and a runtime dependency/SPOF for
  the *default* path. Kept as a pluggable option, not the default.
- **Bespoke claim-predicate config (the #1766 `allowed-groups` shape,
  generalized to a YAML predicate mini-language).** This effectively reinvents a
  worse expression language; CEL subsumes it and is already known to operators.
- **Delegate to Kubernetes RBAC via SubjectAccessReview.** Reuses cluster RBAC,
  but requires mapping OIDC identities to K8s users/groups and per-request SARs;
  awkward for per-agent, claim-driven rules.

## Open Questions

1. **Per-resource policy carrier:** annotation (`kagent.dev/access-policy`,
   zero-schema, matches #1766) vs. a typed `spec.accessPolicy` field
   (validated, discoverable, versioned with the CRD)? Leaning typed field.
2. **Policy combining semantics** — see the UNRESOLVED block above: default-deny +
   allow-if-either, allow-only vs. explicit deny rules.
3. **Scope of per-resource policy initially** — Agent only, or ModelConfig and
   ToolServer too in the first cut?
4. **Default when authorization is enabled but no policy exists** — deny-all
   (secure; #1270 leaned this way) vs. a configurable default rule.
5. **CLI/M2M principals** — how agent-to-controller and service-account calls map
   onto the same policy model (relates to #2071 / STS work).
6. **Should the central policy also be a CRD** (e.g. `AccessPolicy`) rather than a
   ConfigMap, for validation and status?
