# External Bearer Authentication

`external-bearer` is a planned additive authentication mode for API and A2A clients that present bearer credentials directly to kagent.

This slice adds the public configuration surface and Helm rendering only. The controller recognizes the mode name and fails explicitly at startup because the authenticator, outbound validation call, cache, service-actor policy parsing, and A2A policy enforcement are intentionally not implemented yet.

## Boundary

kagent will delegate token validation to one configured external validation service. kagent does not issue tokens, validate token signatures, manage provider-specific issuer/audience policy, or implement provider-specific token protocols.

## Configuration Surface

Controller flags and environment variables:

| Flag | Env var | Description |
|---|---|---|
| `--auth-mode` | `AUTH_MODE` | Set to `external-bearer` to select the planned mode. |
| `--auth-user-id-claim` | `AUTH_USER_ID_CLAIM` | Claim name used for user identity mapping. |
| `--auth-external-bearer-url` | `AUTH_EXTERNAL_BEARER_URL` | External validation endpoint URL. |
| `--auth-external-bearer-timeout` | `AUTH_EXTERNAL_BEARER_TIMEOUT` | Validation request timeout. |
| `--auth-external-bearer-cache-ttl` | `AUTH_EXTERNAL_BEARER_CACHE_TTL` | Maximum validation cache TTL. |
| `--auth-external-bearer-cache-max-entries` | `AUTH_EXTERNAL_BEARER_CACHE_MAX_ENTRIES` | Maximum validation cache entries. |
| `--auth-external-bearer-propagate-token` | `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN` | Whether to forward inbound bearer tokens upstream. |
| `--auth-external-bearer-validation-authorization` | `AUTH_EXTERNAL_BEARER_VALIDATION_AUTHORIZATION` | Authorization header value for validation-service requests. |
| `--auth-external-bearer-policy-file` | `AUTH_EXTERNAL_BEARER_POLICY_FILE` | Local service-actor A2A policy file path. |

Helm exposes the same surface under `controller.auth.externalBearer`. Validation-service authorization is rendered only from a Kubernetes Secret reference; the chart does not accept inline secret values.

## Planned validation contract

The future authenticator will send `POST` requests to `AUTH_EXTERNAL_BEARER_URL` with JSON containing the bearer token without the `Bearer` prefix. Successful responses will map validated identity data into kagent's existing principal model. Failed validation, inactive tokens, network errors, timeouts, malformed responses, and incomplete identity mappings will be treated as unauthenticated.

Bearer propagation is planned to be explicit and opt-in via `AUTH_EXTERNAL_BEARER_PROPAGATE_TOKEN`; user identity forwarding through `X-User-Id` remains the continuity mechanism for A2A/subagent calls.
