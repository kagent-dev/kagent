# External Bearer Authentication

`external-bearer` is an additive authentication mode for API and A2A clients that present bearer credentials directly to kagent. It makes kagent behave like an OAuth protected resource: kagent validates each inbound bearer token by calling one configured OAuth 2.0 Token Introspection endpoint and then applies kagent-local identity mapping and service-actor bounds.

Use this mode when callers can send access tokens directly to kagent and your identity system exposes an RFC 7662-compatible introspection endpoint for those tokens. Use `trusted-proxy` instead when an upstream proxy or gateway owns authentication and forwards already-validated identity to kagent.

## RFC 7662 compatibility boundary

`AUTH_EXTERNAL_BEARER_URL` must point to an RFC 7662-compatible token introspection endpoint. It is not a token endpoint, userinfo endpoint, OIDC discovery URL, or provider-specific debug token-info endpoint.

For each request, kagent sends:

```text
POST <AUTH_EXTERNAL_BEARER_URL>
Content-Type: application/x-www-form-urlencoded
Accept: application/json
Authorization: <configured introspection credential>

token=<bearer-token-without-prefix>&token_type_hint=access_token
```

A compatible response includes `active: true` plus identity and policy inputs such as `sub`, `username`, `client_id`, `scope`, `aud`, `iss`, `grant_type`, and `exp`. kagent preserves introspection response fields in the authenticated principal claims, supports RFC space-separated `scope` strings, and accepts `aud` as either a string or list.

`active: true` means the external auth service has validated the token according to the provider, but it is not sufficient by itself for kagent authentication. `external-bearer` requires a local policy file configured with `AUTH_EXTERNAL_BEARER_POLICY_FILE`; policy validation requires at least one top-level resource-binding control: `requiredScopes`, `allowedAudiences`, or `allowedIssuers`. kagent applies those top-level claim checks before accepting any token, and applies local service-actor A2A policy before allowing bounded service-actor requests.

The introspection URL must use HTTPS for non-localhost hosts. Plain HTTP is accepted only for localhost loopback development or mock validation endpoints.

## Non-goals

`external-bearer` intentionally does not:

- issue tokens, refresh tokens, exchange tokens, or implement OAuth grant flows;
- validate JWT signatures locally or configure JWKS/JWT validation in kagent;
- provide provider-specific adapters, provider registries, or token-info integrations;
- cache introspection responses or expose cache configuration;
- replace `trusted-proxy`;
- authorize broad non-A2A API access for service actors.

Providers without compatible introspection for the tokens sent to kagent should be integrated through a validating proxy, gateway, adapter, or a future local JWT/JWKS mode.

## Configuration

Controller flags and environment variables:

| Flag | Env var | Description |
|---|---|---|
| `--auth-mode` | `AUTH_MODE` | Set to `external-bearer`. |
| `--auth-user-id-claim` | `AUTH_USER_ID_CLAIM` | Claim name used for user identity mapping; defaults to `sub`. |
| `--auth-external-bearer-url` | `AUTH_EXTERNAL_BEARER_URL` | RFC 7662 token introspection endpoint URL. |
| `--auth-external-bearer-timeout` | `AUTH_EXTERNAL_BEARER_TIMEOUT` | Introspection request timeout; defaults to `5s`. |
| `--auth-external-bearer-propagate-token` | `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN` | When `true`, forward the inbound bearer token to upstream agent requests. Defaults to `false`. |
| `--auth-external-bearer-validation-authorization` | `AUTH_EXTERNAL_BEARER_VALIDATION_AUTHORIZATION` | Exact `Authorization` header value for introspection requests. |
| `--auth-external-bearer-client-id` | `AUTH_EXTERNAL_BEARER_CLIENT_ID` | Client ID for RFC 7662 HTTP Basic auth. |
| `--auth-external-bearer-client-secret` | `AUTH_EXTERNAL_BEARER_CLIENT_SECRET` | Client secret for RFC 7662 HTTP Basic auth. |
| `--auth-external-bearer-allow-unauthenticated-introspection` | `AUTH_EXTERNAL_BEARER_ALLOW_UNAUTHENTICATED_INTROSPECTION` | Allow unauthenticated introspection calls; use only for local development/tests. |
| `--auth-external-bearer-policy-file` | `AUTH_EXTERNAL_BEARER_POLICY_FILE` | Required local external-bearer policy file path. |

RFC 7662 response caching is intentionally deferred because token revocation, response freshness, and cache invalidation semantics need a dedicated design; kagent does not expose cache configuration for external-bearer auth yet.

Introspection endpoint authentication is intentionally explicit:

- `validationAuthorization` uses the configured Secret value exactly as the introspection request `Authorization` header.
- `clientId` plus `clientSecret` uses HTTP Basic auth for the introspection endpoint.
- `validationAuthorization` is mutually exclusive with `clientId`/`clientSecret`.
- Partial client credentials are rejected.
- Unauthenticated introspection is rejected unless `allowUnauthenticatedIntrospection` is true and the introspection URL host is localhost/loopback for local/test use.

## Helm values

The chart exposes the mode under `controller.auth.externalBearer`:

```yaml
controller:
  auth:
    mode: external-bearer
    userIdClaim: sub
    externalBearer:
      url: https://auth.example.com/oauth2/introspect
      timeout: 5s
      propagateToken: false
      clientId: kagent-introspection
      clientSecret:
        secretRef:
          name: kagent-introspection-client
          key: client-secret
      policy:
        inline: |
          {
            "requiredScopes": ["kagent:a2a"],
            "allowedAudiences": ["kagent"],
            "serviceActors": {
              "ci-runner": {
                "match": {
                  "allOf": [
                    { "claim": "client_id", "value": "ci-client" },
                    { "claim": "grant_type", "value": "client_credentials" },
                    { "claim": "scope", "contains": "kagent:a2a" }
                  ]
                },
                "allowedA2A": [
                  { "namespace": "kagent", "name": "example-agent", "workloadType": "agent" }
                ]
              }
            }
          }
```

For production, store introspection credentials in Kubernetes Secrets. The chart renders `validationAuthorization.secretRef` and `clientSecret.secretRef` as `valueFrom.secretKeyRef`; it does not accept inline secret values for those sensitive fields.

If you maintain the external-bearer policy outside the release, reference an existing ConfigMap instead:

```yaml
controller:
  auth:
    externalBearer:
      policy:
        existingConfigMap:
          name: kagent-external-bearer-policy
          key: policy.json
```

External-bearer requires a policy source. When inline or existing policy is configured, Helm mounts it at `/etc/kagent/external-bearer/policy.json` and sets `AUTH_EXTERNAL_BEARER_POLICY_FILE`. The policy must include at least one non-empty top-level resource-binding control: `requiredScopes`, `allowedAudiences`, or `allowedIssuers`.

## Service-actor policy

Human-user tokens are authenticated as users only when introspection succeeds, a user identity can be resolved, and the configured policy's top-level resource-binding checks pass. Service actors require explicit local service-actor policy. A service-token-looking credential, such as a token with `grant_type=client_credentials`, must match a configured `serviceActors[*].match.allOf` entry; it must not fall back to user auth through `sub` or `username` if the service policy does not match.

Minimal policy with scope and audience binding:

```json
{"allowedAudiences":["kagent"],"requiredScopes":["kagent:a2a"]}
```

Policy fields:

- `requiredScopes`: top-level scopes that every accepted token must contain when configured.
- `allowedAudiences`: top-level allowed `aud` values; when configured, missing `aud` fails.
- `allowedIssuers`: top-level allowed `iss` values; when configured, missing `iss` fails.

At least one of `requiredScopes`, `allowedAudiences`, or `allowedIssuers` must be non-empty in every external-bearer policy.
- `serviceActors`: map of local service actor IDs to match predicates and A2A allowlists.
- `match.allOf`: at least two exact predicates. `value` is exact scalar/list membership; `contains` is exact scope-token or list membership.
- `allowedA2A`: allowed target triples: `namespace`, `name`, and `workloadType` (`agent`, `sandbox`, or `*`). The `*` wildcard is allowed only as the whole field value.

Each service actor match must include a recognizable service-token indicator predicate. Supported indicators are `grant_type=client_credentials`, or `token_class` / `token_use` with one of `service`, `service_token`, `service-token`, or `client_credentials`. Prefer composite service actor matches such as `client_id` plus `grant_type=client_credentials` plus scope or audience. `client_id` alone is invalid because user-delegated tokens can also carry a client ID.

Expected service-actor behavior:

| Request | Expected result |
|---|---|
| User token with active introspection, user identity, and matching top-level policy | Authenticated as user; A2A-specific service policy does not block it. |
| Inactive, malformed, expired, oversized, or non-2xx introspection response | `401` unauthenticated. |
| Service-token-shaped credential with no matching service-actor policy entry | Denied. This is usually `401` when it cannot be authenticated/classified, or `403` when an authenticated service actor is not allowed for the A2A target. |
| Service token matching policy and allowed target | A2A request allowed. |
| Service token matching policy but different target | `403` denied. |
| Service token to non-A2A API route such as `/api/me` | `403` denied. |

## Local/mock validation recipe

This recipe is for local operator validation only. It uses controller-local loopback HTTP and unauthenticated introspection; do not use this posture for production. Localhost unauthenticated introspection still requires an external-bearer policy file with at least one top-level resource-binding control. The exact `127.0.0.1` URL works when the controller process is running on the same machine as the mock server. For a Helm-deployed controller in Kubernetes, `127.0.0.1` is the controller pod; use an HTTPS mock Service with a trusted certificate, or a pod-local sidecar/loopback proxy, instead of pointing at the operator workstation.

1. Run a mock RFC 7662 endpoint that returns responses by token value:

```python
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs
import json

RESPONSES = {
    "user-token": {
        "active": True,
        "sub": "alice@example.com",
        "username": "alice@example.com",
        "scope": "openid profile kagent:a2a",
        "aud": "kagent",
        "iss": "http://localhost:18080"
    },
    "service-token": {
        "active": True,
        "client_id": "ci-client",
        "grant_type": "client_credentials",
        "scope": "kagent:a2a",
        "aud": "kagent",
        "iss": "http://localhost:18080"
    },
    "inactive-token": {"active": False}
}

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("content-length", "0"))
        body = self.rfile.read(length).decode()
        token = parse_qs(body).get("token", [""])[0]
        data = RESPONSES.get(token, {"active": False})
        encoded = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

HTTPServer(("127.0.0.1", 18080), Handler).serve_forever()
```

2. Configure a locally running controller for local introspection:

```yaml
controller:
  auth:
    mode: external-bearer
    externalBearer:
      url: http://127.0.0.1:18080/introspect
      allowUnauthenticatedIntrospection: true
      propagateToken: false
      policy:
        inline: |
          {
            "requiredScopes": ["kagent:a2a"],
            "allowedAudiences": ["kagent"],
            "serviceActors": {
              "ci-runner": {
                "match": {
                  "allOf": [
                    { "claim": "client_id", "value": "ci-client" },
                    { "claim": "grant_type", "value": "client_credentials" },
                    { "claim": "scope", "contains": "kagent:a2a" }
                  ]
                },
                "allowedA2A": [
                  { "namespace": "kagent", "name": "example-agent", "workloadType": "agent" }
                ]
              }
            }
          }
```

3. Validate expected outcomes:

- `Authorization: Bearer user-token` returns `/api/me` as `alice@example.com`; A2A calls are treated as user calls.
- `Authorization: Bearer inactive-token` is rejected as unauthenticated.
- `Authorization: Bearer service-token` is denied on `/api/me` and other non-A2A routes after it matches service-actor policy. Without a matching service-actor entry, it is denied before target dispatch, typically as unauthenticated when no user identity is resolvable.
- `service-token` is allowed only for `/api/a2a/kagent/example-agent`; a different agent, namespace, or sandbox target returns `403` unless explicitly allowed in policy.
- `kagent invoke --token service-token ...` should succeed only when the invoked A2A target matches `allowedA2A`.
