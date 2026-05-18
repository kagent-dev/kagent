# External Bearer Auth Provider: Plan

## Goal

Add a generic kagent authentication mode for API/A2A clients that present bearer credentials directly to kagent. The mode delegates token validation to one configured external auth or token-introspection service, maps the validated response into kagent's existing `auth.Principal`, and enforces bounded service-actor access to specific A2A targets.

This is an additive kagent auth extension. kagent should not issue tokens, perform provider-specific cryptographic validation, or embed identity-provider policy in this slice.

## Background

### Current kagent auth seams

- Controller auth config currently contains `Config.Auth.Mode` and `Config.Auth.UserIDClaim` (`go/core/pkg/app/app.go:124-128`).
- Runtime flags/env are `--auth-mode`, `--auth-user-id-claim`, `AUTH_MODE`, and `AUTH_USER_ID_CLAIM` (`go/core/pkg/app/app.go:189-190`, `go/core/pkg/app/app.go:224-239`).
- Mode selection is centralized in `getAuthenticator(...)`, currently supporting `trusted-proxy` and `unsecure` (`go/core/cmd/controller/main.go:44-52`).
- The core extension seam is `AuthProvider`, with `Authenticate(...)` for inbound requests and `UpstreamAuth(...)` for outbound propagation (`go/core/pkg/auth/auth.go:48-52`).
- `AuthnMiddleware` applies the configured provider and stores the authenticated session in request context (`go/core/pkg/auth/auth.go:76-95`).
- The same authenticator is injected into HTTP, A2A, and MCP wiring (`go/core/pkg/app/app.go:614-651`).

### Current trusted-proxy behavior

- `ProxyAuthenticator` requires `Authorization: Bearer ...`, parses JWT payload claims without validating the signature, and relies on an upstream proxy to have validated the credential (`go/core/internal/httpserver/auth/proxy_authn.go:28-77`).
- Direct user calls derive `User.ID` from the configured claim, falling back to `sub` (`go/core/internal/httpserver/auth/proxy_authn.go:58-77`).
- Agent-marked calls use `X-Agent-Name` as `Principal.Agent.ID`, and derive `User.ID` from `user_id`, `X-User-Id`, or JWT `sub` (`go/core/internal/httpserver/auth/proxy_authn.go:38-57`).
- Current authenticators retain and forward inbound `Authorization`; they also propagate `X-User-Id` upstream (`go/core/internal/httpserver/auth/authn.go:48-58`, `go/core/internal/httpserver/auth/proxy_authn.go:90-99`).

### A2A identity and policy seams

- kagent's principal model is `Principal{User, Agent, Claims}` (`go/core/pkg/auth/auth.go:19-34`).
- A2A routes are registered under `/api/a2a/{namespace}/{name}` and `/api/a2a-sandboxes/{namespace}/{name}` (`go/core/internal/httpserver/server.go:306-308`).
- `handlerMux` already stores the configured authenticator, extracts namespace/name from the request path, resolves the target handler, and can classify sandbox routes (`go/core/internal/a2a/a2a_handler_mux.go:34-47`, `go/core/internal/a2a/a2a_handler_mux.go:82-112`, `go/core/internal/a2a/a2a_handler_mux.go:114-127`).
- `AuthSessionFrom(ctx)` retrieves the authenticated session placed in request context by middleware (`go/core/pkg/auth/auth.go:59-69`).
- A2A outbound propagation builds an `upstreamPrincipal` with target agent identity before calling `AuthProvider.UpstreamAuth(...)` (`go/core/internal/httpserver/auth/authn.go:82-117`).
- Generic authorization has an extension point through `Authorizer.Check(...)`, although the controller currently wires a no-op authorizer (`go/core/internal/httpserver/handlers/helpers.go:56-79`, `go/core/cmd/controller/main.go:32`). This plan avoids a broad authorization refactor.
- The CLI already supports bearer input for A2A invocation through `--token`, which injects `Authorization: Bearer ...` (`go/core/cli/cmd/kagent/main.go`, `go/core/cli/internal/cli/agent/invoke.go`).

### Helm and documentation seams

- Helm exposes `controller.auth.mode` and `controller.auth.userIdClaim` (`helm/kagent/values.yaml:143-150`).
- `helm/kagent/templates/controller-deployment.yaml:64-68` renders those values into controller env vars.
- The optional oauth2-proxy chart is documented as requiring `controller.auth.mode: trusted-proxy` (`helm/kagent/values.yaml:550-556`).
- oauth2-proxy values already configure bearer-token acceptance and Authorization header forwarding with `skip-jwt-bearer-tokens`, `pass-authorization-header`, and `set-authorization-header` (`helm/kagent/values.yaml:619-640`).
- Chart values comments are currently the main documentation surface for these auth settings.

### Standards and ecosystem precedent

- Envoy external authorization (`ext_authz`) is a standard cloud-native pattern where an external HTTP/gRPC auth service makes allow/deny decisions and can return metadata or header mutations: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter.html
- Kubernetes Gateway API GEP-1494 defines HTTP auth policy attachment and an Envoy-compatible external authorization mode: https://gateway-api.sigs.k8s.io/geps/gep-1494/
- OAuth 2.0 Token Introspection (RFC 7662) defines a protected resource calling an authorization server to determine whether a bearer token is active and to retrieve token metadata: https://www.rfc-editor.org/rfc/rfc7662.html
- oauth2-proxy supports bearer JWT validation for API-style requests through `skip-jwt-bearer-tokens`, `extra-jwt-issuers`, and related options: https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview/

## Approach

### Recommended feature shape

Add a targeted auth mode:

```text
external-bearer
```

The new mode should:

1. Require inbound `Authorization: Bearer <token>`.
2. Call one configured external validation/introspection endpoint.
3. Treat failed validation, inactive tokens, network errors, timeouts, malformed responses, and incomplete identity mappings as unauthenticated.
4. Map successful validation responses into `auth.Principal`.
5. Preserve existing `unsecure` and `trusted-proxy` behavior.
6. Enforce local A2A target policy for validated service actors.

This keeps the change additive and aligned with kagent's existing `AuthProvider` seam. It also keeps token issuance, cryptographic validation, issuer/audience/client policy, provider-specific claims, and revocation semantics outside kagent.

### Ownership boundary

kagent owns:

- extracting inbound bearer credentials;
- calling the configured validation/introspection endpoint;
- validation request timeout, fail-closed behavior, and bounded cache behavior;
- mapping a successful response into `auth.Principal`;
- preserving or suppressing downstream bearer propagation according to config;
- enforcing service-actor A2A target policy;
- Helm values, docs, and tests for the generic mode.

The external auth service owns:

- token issuance;
- cryptographic validation;
- issuer, audience, client, and scope policy;
- token expiry, revocation, and not-before semantics;
- identity proofing;
- provider-specific claim normalization;
- provider-specific protocol details.

### External validation contract

Use one generic HTTP JSON contract for the first slice. Support one configured endpoint, not multiple named providers.

Request:

```text
POST <AUTH_EXTERNAL_BEARER_URL>
Content-Type: application/json
Authorization: <optional configured credential for the validation service>
```

Body:

```json
{
  "token": "<bearer-token-without-prefix>",
  "token_type": "Bearer"
}
```

Response:

```json
{
  "active": true,
  "subject": "user-or-service-subject",
  "user_id": "user@example.com",
  "actor_type": "user",
  "service_actor_id": "",
  "agent_id": "",
  "claims": {
    "sub": "user-or-service-subject"
  },
  "expires_at": 1770000000
}
```

Rules:

- `active: false` is unauthenticated.
- `actor_type` is `user` or `service`; omit means `user`.
- User identity mapping order:
  1. `user_id`
  2. configured `userIdClaim` from `claims`
  3. `claims.sub`
  4. `subject`
- `claims` are copied into `Principal.Claims` exactly as returned; `/api/me` should continue to expose claims when present and fallback to `{sub: Principal.User.ID}` when claims are absent.
- For service actors, `service_actor_id` is required unless `subject` is non-empty.
- The mode must not trust caller-supplied `X-Agent-Name`, `X-User-Id`, or `user_id` as identity sources.
- The mode should not forward inbound request headers, query params, or request paths to the validation endpoint in the first slice.

This contract is intentionally close to token-introspection semantics without requiring kagent to implement provider-specific introspection or JWT validation logic.

### Session representation

Add a private external-bearer session type in the new authenticator package. It should satisfy `auth.Session` and carry:

- `Principal`;
- original bearer token, retained only for optional upstream propagation;
- actor type (`user` or `service`);
- service actor ID, resolved from `service_actor_id` or `subject`;
- response expiry, if supplied.

Do not add service-actor fields to `auth.Principal` in the first slice. Keep service-actor metadata private to the authenticator/session so general principal semantics remain stable.

### Cache behavior

Caching is optional and disabled by default. When enabled:

- cache key is a hash of the inbound bearer token;
- cache expiry is the minimum of configured `CacheTTL` and response `expires_at` when both exist;
- if only configured `CacheTTL` exists, use that TTL;
- if only response `expires_at` exists, use that expiry;
- if neither exists, do not cache;
- `CacheMaxEntries` bounds memory use.

This makes expiry behavior deterministic before implementation starts.

### Service-actor A2A policy

Add a local JSON policy file configured by:

```text
AUTH_EXTERNAL_BEARER_POLICY_FILE
```

Policy shape:

```json
{
  "serviceActors": {
    "service-actor-id": {
      "allowedA2A": [
        {
          "namespace": "kagent",
          "name": "example-agent",
          "workloadType": "agent"
        },
        {
          "namespace": "observability",
          "name": "*",
          "workloadType": "*"
        }
      ]
    }
  }
}
```

Rules:

- `namespace`, `name`, and `workloadType` are required.
- `workloadType` values are `agent`, `sandbox`, or `*`.
- `*` is supported only as the whole field value.
- User actors are allowed through this A2A-specific check.
- Service actors are denied A2A access unless explicitly allowed.
- If no policy file is configured, service actors are denied all A2A access.
- This policy only bounds A2A target access. Broad API authorization is out of scope for this slice.

### Minimal A2A access seam

Keep `AuthProvider` unchanged and add an optional interface in `go/core/pkg/auth/auth.go`:

```go
type A2AWorkloadType string

const (
  A2AWorkloadAgent   A2AWorkloadType = "agent"
  A2AWorkloadSandbox A2AWorkloadType = "sandbox"
)

type A2ATarget struct {
  Namespace    string
  Name         string
  WorkloadType A2AWorkloadType
}

type A2AAccessProvider interface {
  CheckA2AAccess(ctx context.Context, session Session, target A2ATarget) error
}
```

`handlerMux` already stores the configured authenticator. Its `ServeHTTP` path should:

1. extract namespace/name as it does today;
2. resolve the target handler as it does today;
3. derive workload type from `isSandboxRoute(r)`;
4. read the authenticated session with `auth.AuthSessionFrom(r.Context())`;
5. type-assert `a.authenticator` to `auth.A2AAccessProvider`;
6. call `CheckA2AAccess(...)` before dispatching to the per-agent handler;
7. return `403` when the provider denies access.

Existing modes do not implement the optional interface, so their behavior remains unchanged.

### Config shape

Add nested auth config while preserving existing fields:

```go
AuthConfig{
  Mode string
  UserIDClaim string
  ExternalBearer ExternalBearerAuthConfig
}
```

Recommended fields:

- `URL`
- `Timeout`
- `CacheTTL`
- `CacheMaxEntries`
- `PropagateToken`
- `ValidationAuthorization`
- `PolicyFile`

Defaults:

- timeout: `5s`
- cache TTL: `0s` (disabled)
- cache max entries: implementation-defined bounded default
- propagate token: `false`
- policy file: empty

## Work Items

### Item 1 — Add external-bearer config

**Goal:** Introduce typed config fields, flags, and env loading for the new auth mode without changing existing auth behavior.

**Done when:**

- `app.Config.Auth` uses a named `AuthConfig` with existing `Mode` and `UserIDClaim` fields preserved.
- `ExternalBearerAuthConfig` exists with URL, timeout, cache, token propagation, validation authorization, and policy file fields.
- Flags/env load correctly for:
  - `--auth-external-bearer-url` / `AUTH_EXTERNAL_BEARER_URL`
  - `--auth-external-bearer-timeout` / `AUTH_EXTERNAL_BEARER_TIMEOUT`
  - `--auth-external-bearer-cache-ttl` / `AUTH_EXTERNAL_BEARER_CACHE_TTL`
  - `--auth-external-bearer-cache-max-entries` / `AUTH_EXTERNAL_BEARER_CACHE_MAX_ENTRIES`
  - `--auth-external-bearer-propagate-token` / `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN`
  - `--auth-external-bearer-validation-authorization` / `AUTH_EXTERNAL_BEARER_VALIDATION_AUTHORIZATION`
  - `--auth-external-bearer-policy-file` / `AUTH_EXTERNAL_BEARER_POLICY_FILE`
- Existing `unsecure` and `trusted-proxy` defaults remain unchanged.

**Key files:**

- `go/core/pkg/app/app.go`
- `go/core/cmd/controller/main.go`
- `go/core/cmd/controller/auth_mode_test.go`

**Dependencies:** None.

**Size:** Medium.

### Item 2 — Implement `ExternalBearerAuthenticator`

**Goal:** Add an authenticator that validates inbound bearer tokens through the configured external endpoint and maps successful responses into `auth.Principal`.

**Done when:**

- `ExternalBearerAuthenticator` implements `auth.AuthProvider`.
- A private external-bearer session type carries principal, actor type, service actor ID, original bearer token, and response expiry.
- Missing or non-bearer `Authorization` returns unauthenticated.
- The authenticator sends the generic JSON validation request.
- It fails closed on timeout, network errors, invalid JSON, inactive tokens, and incomplete identity mappings.
- It maps `Principal.User.ID`, optional `Principal.Agent.ID`, and raw `Principal.Claims` from the validation response.
- It does not trust `X-Agent-Name`, `X-User-Id`, or `user_id` query params as identity sources.
- Cache behavior follows the deterministic rules in this plan.
- Unit tests cover active/inactive tokens, malformed responses, missing identity, service actor metadata, cache keying, and expiry precedence.

**Key files:**

- Add `go/core/internal/httpserver/auth/external_bearer_authn.go`
- Add `go/core/internal/httpserver/auth/external_bearer_authn_test.go`
- `go/core/cmd/controller/main.go`
- `go/core/internal/httpserver/handlers/current_user.go` as a regression surface for `/api/me`

**Dependencies:** Item 1.

**Size:** Large.

### Item 3 — Define upstream propagation behavior

**Goal:** Preserve user continuity downstream while making bearer-token forwarding explicit and opt-in.

**Done when:**

- `UpstreamAuth` always forwards `X-User-Id: <Principal.User.ID>`.
- `Authorization` is forwarded only when `PropagateToken` is true.
- The authenticator does not mint, exchange, or transform tokens.
- Existing `unsecure` and `trusted-proxy` propagation behavior remains unchanged.
- Tests cover both `PropagateToken: true` and `PropagateToken: false`.
- MCP-to-A2A invocation remains a regression surface because it uses the same A2A request handler path.

**Key files:**

- `go/core/internal/httpserver/auth/external_bearer_authn.go`
- `go/core/internal/httpserver/auth/external_bearer_authn_test.go`
- Regression surfaces:
  - `go/core/internal/httpserver/auth/authn.go`
  - `go/core/internal/a2a/a2a_registrar.go`
  - `go/core/internal/mcp/mcp_handler.go`

**Dependencies:** Item 2.

**Size:** Medium.

### Item 4 — Add optional A2A access-check interface

**Goal:** Add a narrow A2A target authorization seam without changing the existing `AuthProvider` contract or refactoring general API authorization.

**Done when:**

- `auth.A2ATarget`, `auth.A2AWorkloadType`, and `auth.A2AAccessProvider` exist.
- `handlerMux.ServeHTTP` reads the authenticated session from request context and uses the stored authenticator for the optional type assertion.
- `handlerMux.ServeHTTP` extracts target namespace/name and workload type before dispatch.
- `handlerMux.ServeHTTP` calls `CheckA2AAccess` only when the configured authenticator implements `A2AAccessProvider`.
- Denied A2A access returns `403`.
- Existing auth modes continue to allow A2A because they do not implement the optional interface.

**Key files:**

- `go/core/pkg/auth/auth.go`
- `go/core/internal/a2a/a2a_handler_mux.go`
- Add or extend `go/core/internal/a2a/a2a_handler_mux_test.go`

**Dependencies:** Item 1. This interface/mux hook can be developed with a fake provider before `ExternalBearerAuthenticator` is complete.

**Size:** Medium.

### Item 5 — Load and enforce service-actor A2A policy

**Goal:** Allow `ExternalBearerAuthenticator` to enforce bounded A2A access for validated service actors.

**Done when:**

- Policy file is loaded when constructing `ExternalBearerAuthenticator`.
- Invalid policy JSON fails controller startup.
- Service actors are denied A2A by default.
- User actors pass the A2A-specific policy check.
- Exact matching and whole-field `*` wildcards work for namespace, name, and workload type.
- Tests cover no policy file, allowed exact target, denied target, wildcard target, sandbox target, malformed policy, and missing session metadata.

**Key files:**

- `go/core/internal/httpserver/auth/external_bearer_authn.go`
- `go/core/internal/httpserver/auth/external_bearer_authn_test.go`
- `go/core/internal/a2a/a2a_handler_mux.go`

**Dependencies:** Items 2, 4, and 6. Mode construction must pass loaded policy into the authenticator.

**Size:** Medium.

### Item 6 — Wire mode selection

**Goal:** Make `external-bearer` selectable at controller startup and keep unknown-mode behavior clear.

**Done when:**

- `getAuthenticator` supports `unsecure`, `trusted-proxy`, and `external-bearer`.
- `external-bearer` startup fails if required URL config is missing.
- `external-bearer` construction wires timeout/cache/propagation/validation-service credential/policy settings.
- `auth_mode_test.go` asserts the new mode returns `ExternalBearerAuthenticator`.
- Unknown-mode panic/error text lists all valid modes.

**Key files:**

- `go/core/cmd/controller/main.go`
- `go/core/cmd/controller/auth_mode_test.go`

**Dependencies:** Items 1 and 2.

**Size:** Small.

### Item 7 — Update E2E auth-mode detection

**Goal:** Prevent existing E2E tests from misclassifying secure non-`trusted-proxy` modes as `unsecure`.

**Done when:**

- `detectAuthMode` distinguishes `trusted-proxy`, `unsecure`, and secure/unknown modes.
- Existing unsecure tests skip when unauthenticated `/api/me` returns `401`.
- Existing trusted-proxy tests still run only when unsigned probe JWT behavior matches trusted-proxy expectations.
- Optional external-bearer E2E coverage is guarded by generic env-provided token and expected subject, not provider-specific setup.
- `/api/me` behavior is covered for external-bearer claims/fallback semantics.

**Key files:**

- `go/core/test/e2e/auth_api_test.go`

**Dependencies:** Item 6.

**Size:** Small.

### Item 8 — Expose Helm values and rendering

**Goal:** Make the generic mode deployable through the chart without embedding provider-specific settings.

**Done when:**

- `helm/kagent/values.yaml` adds `controller.auth.externalBearer` fields for URL, timeout, cache, token propagation, validation-service credential secret ref, and service-actor policy.
- `controller-deployment.yaml` renders env vars for external-bearer fields.
- Sensitive validation-service authorization is rendered only from a Secret ref, not inline values.
- Inline or existing ConfigMap policy is mounted at a fixed implementation-chosen path.
- `AUTH_EXTERNAL_BEARER_POLICY_FILE` is set when a policy is mounted.
- Helm tests cover default absence, mode/env rendering, Secret ref rendering, policy ConfigMap rendering, and mount wiring.

**Key files:**

- `helm/kagent/values.yaml`
- `helm/kagent/templates/controller-deployment.yaml`
- Add `helm/kagent/templates/external-bearer-policy-configmap.yaml`
- `helm/kagent/tests/controller-deployment_test.yaml`

**Dependencies:** Item 1.

**Size:** Medium.

### Item 9 — Update docs

**Goal:** Document the new generic contract and boundaries so operators can choose between `trusted-proxy` and `external-bearer` correctly.

**Done when:**

- Documentation covers the mode name, required env/Helm fields, validation request/response contract, identity mapping rules, propagation behavior, and service-actor policy format.
- Existing trusted-proxy docs are clarified as documenting the upstream-proxy-authenticated boundary, not the only secure auth mode.
- A2A/subagent docs mention that external-bearer continues to forward `X-User-Id`, while bearer forwarding is configurable.
- Docs explicitly state that token issuance and cryptographic validation remain the external auth service's responsibility.

**Key files:**

- `docs/OIDC_PROXY_AUTH_ARCHITECTURE.md`
- `docs/architecture/a2a-subagents.md`
- Optionally add `docs/architecture/external-bearer-auth.md`

**Dependencies:** Items 2, 3, 5, and 8.

**Size:** Medium.

### Item 10 — Validate behavior

**Goal:** Confirm the new mode is covered without regressing existing auth modes.

**Done when:**

- Go unit tests pass for auth implementations, mode selection, and A2A mux policy behavior.
- Helm unit tests pass for the new chart values and mounts.
- Validation checklist is recorded:
  - `unsecure` unchanged;
  - `trusted-proxy` unchanged;
  - `external-bearer` user token accepted through a mock validation service;
  - inactive token rejected;
  - service actor denied without policy;
  - service actor allowed only for configured A2A target;
  - CLI `kagent invoke --token` works against an allowed A2A target;
  - MCP-to-A2A path still propagates user identity through the same request handler path;
  - `/api/me` returns mapped claims/fallback as expected.

**Key files:** Test files from prior items.

**Dependencies:** All prior items.

**Size:** Small.

## Non-goals

- Supporting multiple external auth providers in the first slice.
- Implementing provider-specific JWT/JWKS validation inside kagent.
- Implementing token issuance, token refresh, token exchange, or OAuth grant flows.
- Replacing `trusted-proxy`.
- Refactoring broad API authorization.
- Allowing service actors to access arbitrary non-A2A API routes.

## Open Questions

None blocking for the first implementation slice. The plan chooses a single-provider external-bearer mode, fixed JSON token validation request, private service-actor session metadata, deterministic cache expiry, and A2A-only service-actor policy.

## References

- `go/core/pkg/auth/auth.go:19-95`
- `go/core/cmd/controller/main.go:31-52`
- `go/core/internal/httpserver/auth/proxy_authn.go:28-99`
- `go/core/internal/httpserver/auth/authn.go:82-117`
- `go/core/internal/a2a/a2a_handler_mux.go:34-47`, `go/core/internal/a2a/a2a_handler_mux.go:82-127`
- `go/core/pkg/app/app.go:124-128`, `go/core/pkg/app/app.go:189-190`, `go/core/pkg/app/app.go:614-651`
- `helm/kagent/values.yaml:143-150`, `helm/kagent/values.yaml:550-640`
- Envoy external authorization filter: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter.html
- Gateway API GEP-1494 HTTP Auth: https://gateway-api.sigs.k8s.io/geps/gep-1494/
- RFC 7662 OAuth 2.0 Token Introspection: https://www.rfc-editor.org/rfc/rfc7662.html
- oauth2-proxy configuration overview: https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview/
