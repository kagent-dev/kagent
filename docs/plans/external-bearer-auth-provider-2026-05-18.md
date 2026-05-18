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

### Current branch reconciliation

- The aggregate branch already contains an `ExternalBearerAuthenticator` slice that posts a custom JSON validation request and parses a normalized JSON response (`go/core/internal/httpserver/auth/external_bearer_authn.go`). That was useful as an initial proof seam, but it should not be carried forward.
- Before adding service-actor policy, replace the custom JSON validation contract with RFC 7662 token introspection: form-encoded `token`/`token_type_hint`, RFC introspection response fields, bounded response reads, and introspection endpoint authentication.
- The existing config surface does not need a protocol selector if RFC 7662 is the only supported first implementation. Keep the mode smaller and avoid advertising a custom validator protocol.

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
- enforcing service-actor request bounds, including A2A target policy and default-deny non-A2A service-actor access;
- Helm values, docs, and tests for the generic mode.

The external auth service owns:

- token issuance;
- cryptographic validation;
- token expiry, revocation, and not-before semantics;
- identity proofing;
- provider-specific protocol details;
- provider-native token metadata such as issuer, audience, scopes, client identity, subject, username, and grant type.

For providers that expose an RFC 7662-compatible token introspection endpoint for the tokens presented to kagent, no provider-specific adapter is required. kagent consumes the introspection response and applies generic local bounds where needed: required scopes/audiences/issuers when configured, service-actor identity mapping from configured claims, and A2A target allowlists. Providers that do not expose compatible introspection should be integrated through a validating proxy, gateway, or adapter, or through a future local JWT/JWKS mode.

The validation/introspection service must return `active: true` only when the token is cryptographically valid, time-valid, and not revoked according to the provider. kagent then verifies that the active token is acceptable for the configured kagent deployment/API using generic claim checks and local service-actor policy. This keeps provider-specific validation outside kagent while avoiding a custom adapter requirement for standard OAuth deployments.

### External validation protocol

Support one configured validator endpoint, not multiple named identity providers. The mode should be provider-neutral by using the OAuth 2.0 Token Introspection contract rather than provider-specific integrations.

Initial supported protocol:

- `rfc7662`: OAuth 2.0 Token Introspection request/response handling for providers that expose a compatible introspection endpoint.

This makes the first implementation standards-based for upstream users whose providers expose RFC 7662-compatible introspection and avoids a custom validation protocol. It does not claim direct compatibility with every identity provider; JWT/JWKS-only providers remain future work or require an external validating adapter. kagent still does not own provider-specific token validation, token issuance, or OAuth grant flows.

### Standards-first deployment model

The plan should optimize for the common OAuth protected-resource model:

1. A caller service has a confidential OAuth client that can obtain short-lived access tokens through `client_credentials`.
2. kagent has a separate confidential OAuth client used only to authenticate to the configured token introspection endpoint.
3. kagent validates inbound bearer tokens through RFC 7662 token introspection.
4. kagent maps standard introspection fields such as `client_id`, `sub`, `username`, `scope`, `aud`, `iss`, and `exp` into `auth.Principal` and local policy inputs.
5. kagent enforces local service-actor bounds before dispatching to A2A targets.

This deployment model requires no browser login flow and no user-session OAuth features. The OAuth clients used for service-to-service flows should be confidential clients with the smallest required grant set, normally `client_credentials` only. kagent should not require redirect URIs, authorization-code grants, ID tokens, refresh tokens, or interactive callback semantics for this path.

That is also the maintainer-facing value proposition: `external-bearer` makes kagent usable as an OAuth protected resource for API/A2A clients using standard token introspection.

#### RFC 7662 token introspection

For OAuth 2.0 Token Introspection, kagent sends a form-encoded request:

```text
POST <AUTH_EXTERNAL_BEARER_URL>
Content-Type: application/x-www-form-urlencoded
Accept: application/json
Authorization: <optional configured credential for the introspection endpoint>
```

Body:

```text
token=<bearer-token-without-prefix>&token_type_hint=access_token
```

Authentication to the introspection endpoint should support the existing generic validation-service authorization header and a standard client-credentials shape for RFC 7662 deployments:

- `clientId`
- `clientSecret` from a Secret/env source
- Basic auth derived from `clientId:clientSecret` when both are configured

Credential precedence must be explicit:

- if `ValidationAuthorization` is set, use that value exactly as the introspection `Authorization` header and reject simultaneous `ClientID`/`ClientSecret` config;
- otherwise, if `ClientID` and `ClientSecret` are set, use HTTP Basic auth;
- otherwise, fail startup unless unauthenticated introspection is explicitly enabled for tests or local development.

The implementation should cap the introspection response body size before JSON decoding; 64 KiB is sufficient for normal introspection responses and prevents unbounded reads from a configured external endpoint. The introspection URL should require HTTPS by default for non-localhost endpoints.

Response mapping uses the same internal validation model:

- `active` maps to validation activity; `false` or missing active is unauthenticated.
- `username` is a user ID candidate.
- `sub` is the subject and user ID fallback.
- `client_id` is a service-actor policy input for client-credentials/service-token use cases, but not sufficient by itself to classify a service actor.
- `exp` maps to `expires_at`.
- `scope`, `aud`, `iss`, `grant_type`, and any additional response fields are preserved in `Principal.Claims`.
- `aud` matching must support both string and list-of-strings response forms.
- `scope` matching should implement the RFC space-separated string form and may tolerate provider-specific array forms as an extension.

For client-credentials service actors, `client_id` is a useful identity input but must not be the only proof that the token represents a service actor. User-delegated tokens can also contain a `client_id`. Service-actor classification therefore requires a composite policy match, such as `client_id` plus `grant_type=client_credentials`, a kagent-specific scope, an audience, or another deployment-specific token-class claim. If a provider exposes a different stable client identifier, the policy should be able to match that claim without changing Go code.

#### Generic post-introspection checks

Direct RFC 7662 integration should not depend on a custom validator returning deployment-specific JSON. To keep the mode safe and broadly reusable, kagent should support generic checks over the introspection response before accepting the session:

- required scopes, matched against the RFC 7662 `scope` string or equivalent claim;
- allowed audiences, matched against `aud` when present;
- allowed issuers, matched against `iss` when present;
- service-actor allowlists keyed by composite claim predicates such as `client_id` plus `grant_type`, `scope`, `aud`, or another scalar claim returned by introspection.

These checks are optional for human-user authentication. Service-actor classification is stricter: service actors require explicit local policy and at least one positive kagent-specific binding, such as required scope, allowed audience, issuer plus client identity, or another configured token-class claim. If no service-actor policy matches, the request remains a user-authenticated request when a user identity is resolvable, or is rejected when no user identity is resolvable.

For RFC 7662, `active: true` is necessary but not sufficient for service-actor access. kagent still applies configured generic claim checks and local A2A policy before allowing bounded service-actor traffic.

### Session representation

Add a private external-bearer session type in the new authenticator package. It should satisfy `auth.Session` and carry:

- `Principal`;
- original bearer token, retained only in the later propagation slice when optional upstream propagation is enabled;
- actor type (`user` or `service`);
- service actor ID, resolved from the matched service-actor policy entry;
- response expiry, if supplied.

Do not add service-actor fields to `auth.Principal` in the first slice. Keep service-actor metadata private to the authenticator/session so general principal semantics remain stable.

### Cache behavior

Caching is optional and disabled by default. When enabled:

- cache key is a hash of the inbound bearer token;
- cache expiry is the minimum of configured `CacheTTL` and response `expires_at` when both exist;
- if only configured `CacheTTL` exists, use that TTL;
- if only response `expires_at` exists, use that expiry;
- if neither exists, do not cache;
- inactive responses, validation errors, and responses without a positive identity mapping are not cached by default;
- `CacheMaxEntries` bounds memory use;
- metrics should distinguish cache hit, miss, expired entry, inactive token, and validation error outcomes.

This makes expiry behavior deterministic before implementation starts.

### Service-actor A2A policy

Add a local JSON policy file configured by:

```text
AUTH_EXTERNAL_BEARER_POLICY_FILE
```

Policy shape:

```json
{
  "requiredScopes": ["kagent:a2a"],
  "allowedAudiences": ["kagent"],
  "allowedIssuers": ["https://issuer.example.com"],
  "serviceActors": {
    "service-actor-id": {
      "match": {
        "allOf": [
          { "claim": "client_id", "value": "service-client-id" },
          { "claim": "grant_type", "value": "client_credentials" },
          { "claim": "scope", "contains": "kagent:a2a" }
        ]
      },
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

- Top-level `requiredScopes`, `allowedAudiences`, and `allowedIssuers` are optional for user authentication. Service actors require explicit local policy and at least one positive kagent-specific binding through scope, audience, issuer plus client identity, or another configured claim.
- `serviceActors` maps a local stable actor ID to provider-neutral claim predicates.
- `match.allOf` is required for service actors and contains one or more predicate objects.
- Supported predicate operators are `value` for exact scalar/list membership and `contains` for scope membership or list membership.
- `client_id` alone is not sufficient to classify a service actor unless the deployment can prove that client can only issue client-credentials tokens; prefer matching `client_id` plus `grant_type`, scope, audience, or another token-class claim.
- `namespace`, `name`, and `workloadType` are required for each `allowedA2A` target.
- `workloadType` values are `agent`, `sandbox`, or `*`.
- `*` is supported only as the whole field value.
- User actors are allowed through this A2A-specific check.
- Service actors are denied A2A access unless explicitly matched and allowed.
- If no policy file is configured, service actors are denied all A2A access.
- Service actors are denied non-A2A API access by default in this plan.
- The first policy implementation should bound A2A targets and add the default-deny non-A2A service-actor guard; broad human-user API authorization remains out of scope.

### Non-A2A service-actor deny seam

The A2A mux hook only protects A2A dispatch. It does not see `/api/me`, general REST APIs, admin APIs, or future non-A2A routes. To make the service-actor default-deny guarantee real, add a concrete post-auth guard in `AuthnMiddleware` or a new middleware directly after authentication:

```text
if session is an external-bearer service actor
  and request path is not /api/a2a/*
  and request path is not /api/a2a-sandboxes/*
then return 403
```

This guard should run before general API handlers. Existing user sessions and existing auth modes are unaffected. A2A and A2A sandbox paths continue into the A2A mux, where the target-specific `A2AAccessProvider` check applies.

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
- `ClientID`
- `ClientSecret` or `ClientSecretRef` at deployment/rendering layers
- `AllowUnauthenticatedIntrospection` for tests/local development only
- `PolicyFile`

Defaults:

- timeout: `5s`
- cache TTL: `0s` (disabled)
- cache max entries: implementation-defined bounded default
- propagate token: `false`
- validation authorization: empty
- client ID/client secret: empty
- unauthenticated introspection: `false`
- policy file: empty

## Work Items

### Item 1 — Add external-bearer config

**Goal:** Introduce typed config fields, flags, and env loading for the new auth mode without changing existing auth behavior.

**Done when:**

- `app.Config.Auth` uses a named `AuthConfig` with existing `Mode` and `UserIDClaim` fields preserved.
- `ExternalBearerAuthConfig` exists with URL, timeout, cache, token propagation, validation authorization, RFC 7662 client authentication, unauthenticated-introspection opt-in, and policy file fields.
- Startup rejects ambiguous introspection auth config, including simultaneous `ValidationAuthorization` and `ClientID`/`ClientSecret`.
- Startup rejects unauthenticated introspection unless the explicit test/local-development opt-in is set.
- Flags/env load correctly for:
  - `--auth-external-bearer-url` / `AUTH_EXTERNAL_BEARER_URL`
  - `--auth-external-bearer-timeout` / `AUTH_EXTERNAL_BEARER_TIMEOUT`
  - `--auth-external-bearer-cache-ttl` / `AUTH_EXTERNAL_BEARER_CACHE_TTL`
  - `--auth-external-bearer-cache-max-entries` / `AUTH_EXTERNAL_BEARER_CACHE_MAX_ENTRIES`
  - `--auth-external-bearer-propagate-token` / `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN`
  - `--auth-external-bearer-validation-authorization` / `AUTH_EXTERNAL_BEARER_VALIDATION_AUTHORIZATION`
  - `--auth-external-bearer-client-id` / `AUTH_EXTERNAL_BEARER_CLIENT_ID`
  - `--auth-external-bearer-client-secret` / `AUTH_EXTERNAL_BEARER_CLIENT_SECRET`
  - `--auth-external-bearer-allow-unauthenticated-introspection` / `AUTH_EXTERNAL_BEARER_ALLOW_UNAUTHENTICATED_INTROSPECTION`
  - `--auth-external-bearer-policy-file` / `AUTH_EXTERNAL_BEARER_POLICY_FILE`
- Existing `unsecure` and `trusted-proxy` defaults remain unchanged.

**Key files:**

- `go/core/pkg/app/app.go`
- `go/core/cmd/controller/main.go`
- `go/core/cmd/controller/auth_mode_test.go`

**Dependencies:** None.

**Size:** Medium.

### Item 2 — Replace custom JSON validation with RFC 7662 authenticator

**Goal:** Make `ExternalBearerAuthenticator` validate inbound bearer tokens through RFC 7662 token introspection and map successful introspection responses into `auth.Principal`. On the aggregate branch, this means replacing the existing custom JSON request/response proof implementation rather than extending it.

**Done when:**

- `ExternalBearerAuthenticator` implements `auth.AuthProvider`.
- A private external-bearer session type carries principal, actor type, service actor ID, and response expiry.
- Missing or non-bearer `Authorization` returns unauthenticated.
- The authenticator sends an RFC 7662 form-encoded token introspection request.
- RFC 7662 requests set `Accept: application/json`, cap response reads before decoding, and use Basic auth when client ID and client secret are configured.
- It requires HTTPS for non-localhost introspection endpoints.
- It fails closed on timeout, network errors, invalid JSON, missing or false `active`, non-2xx responses, oversized responses, and incomplete identity mappings.
- It maps `Principal.User.ID`, optional `Principal.Agent.ID`, and raw `Principal.Claims` from the validation response.
- It does not trust `X-Agent-Name`, `X-User-Id`, or `user_id` query params as identity sources.
- Cache behavior is not implemented in this item; cache config remains reserved for a later slice.
- Unit tests cover active/inactive tokens, missing `active`, malformed responses, unsupported content, `401`/`403`/`500` responses, slow responses, oversized responses, missing identity, service actor metadata, identity fallback order, RFC 7662 request shape, Basic auth, auth-header precedence conflicts, HTTPS enforcement, RFC 7662 field mapping, `aud` string/list handling, `scope` string handling, and preservation of `client_id`/`scope`/`aud`/`iss` claims for later policy evaluation.

**Key files:**

- Add `go/core/internal/httpserver/auth/external_bearer_authn.go`
- Add `go/core/internal/httpserver/auth/external_bearer_authn_test.go`
- `go/core/cmd/controller/main.go`
- `go/core/internal/httpserver/handlers/current_user.go` as a regression surface for `/api/me`

**Dependencies:** Item 1.

**Size:** Large.

### Item 3 — Define upstream propagation behavior

**Goal:** Preserve user continuity downstream while making bearer-token forwarding explicit and preventing service actors from being silently represented as human users.

**Done when:**

- `UpstreamAuth` forwards `X-User-Id: <Principal.User.ID>` for user actors.
- Service actors do not receive a human-looking `X-User-Id` by default; if a downstream continuity header is needed, it uses explicit service-actor semantics such as a distinguishable `service:<id>` subject or separate actor metadata headers.
- `Authorization` is forwarded only when `PropagateToken` is true.
- The authenticator does not mint, exchange, or transform tokens.
- Existing `unsecure` and `trusted-proxy` propagation behavior remains unchanged.
- Tests cover user propagation, service-actor propagation behavior, and both `PropagateToken: true` and `PropagateToken: false`.
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

### Item 5 — Load and enforce service-actor request policy

**Goal:** Allow `ExternalBearerAuthenticator` to classify service actors through composite claim predicates, enforce bounded A2A access, and default-deny service actors on non-A2A API routes.

**Done when:**

- Policy file is loaded when constructing `ExternalBearerAuthenticator`.
- Invalid policy JSON fails controller startup.
- Optional `requiredScopes`, `allowedAudiences`, and `allowedIssuers` are evaluated against validation/introspection claims before the session is accepted.
- Service actors are identified by matching composite configured claim predicates such as `client_id` plus `grant_type`, scope, audience, or another token-class claim, not by trusting caller-supplied headers.
- A token that matches only `client_id` is not classified as a service actor unless the policy explicitly documents and tests that the client is service-only.
- Service actors are denied A2A by default.
- A concrete post-auth middleware or `AuthnMiddleware` guard denies external-bearer service actors on non-A2A paths before general API handlers run.
- User actors pass the A2A-specific policy check and continue through normal API auth behavior.
- Exact matching and whole-field `*` wildcards work for namespace, name, and workload type.
- Tests cover no policy file, required-scope pass/fail, audience/issuer pass/fail, composite service actor claim match, client-id-only non-match, allowed exact target, denied A2A target, denied non-A2A API route, wildcard target, sandbox target, malformed policy, and missing session metadata.

**Key files:**

- `go/core/internal/httpserver/auth/external_bearer_authn.go`
- `go/core/internal/httpserver/auth/external_bearer_authn_test.go`
- `go/core/internal/httpserver/auth/authn.go` or a new post-auth middleware for the non-A2A service-actor deny guard
- `go/core/internal/a2a/a2a_handler_mux.go`

**Dependencies:** Items 2, 4, and 6. Mode construction must pass loaded policy into the authenticator.

**Size:** Medium.

### Item 6 — Wire mode selection

**Goal:** Make `external-bearer` selectable at controller startup and keep unknown-mode behavior clear.

**Done when:**

- `getAuthenticator` supports `unsecure`, `trusted-proxy`, and `external-bearer`.
- `external-bearer` startup fails if required URL config is missing.
- `external-bearer` construction wires the settings implemented in the current slice; later cache, propagation, and policy slices extend construction without changing the mode name.
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

- `helm/kagent/values.yaml` adds `controller.auth.externalBearer` fields for RFC 7662 introspection endpoint URL, timeout, cache, token propagation, validation-service credential secret ref, RFC 7662 client credential Secret ref, unauthenticated-introspection test/local opt-in, and service-actor policy.
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

- Documentation covers the mode name, required env/Helm fields, RFC 7662 token introspection behavior, identity mapping rules, propagation behavior, and service-actor policy format.
- Docs present `external-bearer` as a standards-based OAuth protected-resource mode using RFC 7662 token introspection for providers that expose compatible introspection for the tokens sent to kagent.
- Docs state that providers without compatible introspection need a validating proxy/gateway/adapter or a future JWT/JWKS mode.
- Docs state that `AUTH_EXTERNAL_BEARER_URL` points to an RFC 7662-compatible token introspection endpoint, not a token endpoint, userinfo endpoint, discovery URL, or debug tokeninfo endpoint.
- Docs state that `active: true` means provider validation succeeded, and kagent still applies configured generic claim checks plus service-actor policy before accepting/bounding the request.
- Existing trusted-proxy docs are clarified as documenting the upstream-proxy-authenticated boundary, not the only secure auth mode.
- A2A/subagent docs mention that external-bearer forwards human user continuity separately from service-actor identity, while bearer forwarding is configurable.
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
  - MCP-to-A2A path still propagates human user identity through the same request handler path;
  - `/api/me` returns mapped claims/fallback as expected.

**Key files:** Test files from prior items.

**Dependencies:** All prior items.

**Size:** Small.

## Non-goals

- Supporting multiple external auth providers in the first slice.
- Implementing provider-specific integrations, provider registries, or JWT/JWKS validation inside kagent.
- Implementing token issuance, token refresh, token exchange, or OAuth grant flows.
- Replacing `trusted-proxy`.
- Refactoring broad API authorization.
- Allowing service actors to access arbitrary non-A2A API routes.

## Open Questions

None blocking for the first implementation slice. The plan chooses a single-validator external-bearer mode using RFC 7662 token introspection, private service-actor session metadata, composite service-actor policy matching, deterministic cache expiry, and a concrete non-A2A service-actor deny guard. Provider-specific integrations, provider registries, local JWT/JWKS validation, and custom validator protocols remain out of scope.

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
