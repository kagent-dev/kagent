# External Bearer Authentication

`external-bearer` is a planned additive authentication mode for API and A2A clients that present bearer credentials directly to kagent.

This slice adds the public configuration surface and Helm rendering only. The controller recognizes the mode name and fails explicitly at startup because the authenticator, RFC 7662 introspection call, cache, service-actor policy parsing, non-A2A service-actor deny guard, and A2A policy enforcement are intentionally not implemented yet.

## Boundary

kagent will act as an OAuth protected resource. It will validate inbound bearer tokens by calling one configured RFC 7662-compatible token introspection endpoint.

kagent does not issue tokens, implement OAuth grant flows, validate token signatures locally, or embed provider-specific JWT/JWKS integrations in this mode. Providers that do not expose compatible introspection for the tokens presented to kagent should be integrated through a validating proxy, gateway, adapter, or a future local JWT/JWKS mode.

## Current configuration surface

Controller flags and environment variables currently reserved by the config/Helm slice:

| Flag | Env var | Description |
|---|---|---|
| `--auth-mode` | `AUTH_MODE` | Set to `external-bearer` to select the planned mode. |
| `--auth-user-id-claim` | `AUTH_USER_ID_CLAIM` | Claim name used for user identity mapping. |
| `--auth-external-bearer-url` | `AUTH_EXTERNAL_BEARER_URL` | RFC 7662-compatible token introspection endpoint URL. |
| `--auth-external-bearer-timeout` | `AUTH_EXTERNAL_BEARER_TIMEOUT` | Introspection request timeout. |
| `--auth-external-bearer-cache-ttl` | `AUTH_EXTERNAL_BEARER_CACHE_TTL` | Maximum introspection cache TTL; planned, not active until cache implementation lands. |
| `--auth-external-bearer-cache-max-entries` | `AUTH_EXTERNAL_BEARER_CACHE_MAX_ENTRIES` | Maximum introspection cache entries; planned, not active until cache implementation lands. |
| `--auth-external-bearer-propagate-token` | `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN` | Whether to forward inbound bearer tokens upstream; planned, not active until authenticator implementation lands. |
| `--auth-external-bearer-validation-authorization` | `AUTH_EXTERNAL_BEARER_VALIDATION_AUTHORIZATION` | Authorization header value for introspection requests. Future slices may replace or complement this with explicit client ID/client secret config. |
| `--auth-external-bearer-policy-file` | `AUTH_EXTERNAL_BEARER_POLICY_FILE` | Local service-actor A2A policy file path. |

Helm exposes the same current surface under `controller.auth.externalBearer`. Validation-service authorization is rendered only from a Kubernetes Secret reference; the chart does not accept inline secret values.

## Planned RFC 7662 validation contract

The future authenticator will send RFC 7662 token introspection requests to `AUTH_EXTERNAL_BEARER_URL`:

```text
POST <AUTH_EXTERNAL_BEARER_URL>
Content-Type: application/x-www-form-urlencoded
Accept: application/json
Authorization: <configured introspection credential>

token=<bearer-token-without-prefix>&token_type_hint=access_token
```

Successful introspection responses will map standard fields such as `active`, `username`, `sub`, `client_id`, `scope`, `aud`, `iss`, `grant_type`, and `exp` into kagent's existing principal model and service-actor policy inputs. Failed introspection, inactive tokens, network errors, timeouts, malformed responses, missing `active`, non-2xx responses, oversized responses, and incomplete identity mappings will be treated as unauthenticated.

`active: true` is necessary but not sufficient for service-actor access. kagent will still apply configured claim checks and local service-actor A2A policy before allowing bounded service-actor traffic.

## Service actors and propagation

Human user continuity may continue to use `X-User-Id` for A2A/subagent calls.

Service actors should not receive a human-looking `X-User-Id` by default. They require explicit service-actor semantics and a composite policy match, such as `client_id` plus `grant_type=client_credentials` plus a kagent-specific scope or audience. A service/client-credentials token that fails service-actor policy must be rejected rather than falling back to user authentication through `sub`.

Bearer propagation is planned to be explicit and opt-in via `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN`.
