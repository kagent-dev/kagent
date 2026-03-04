# External Authorization Guide

This document explains kagent's authorization architecture and how to integrate an external policy engine using the **provider adapter layer**.

## Overview

Kagent uses an `Authorizer` interface to decouple authorization decisions from HTTP handlers. The `ExternalAuthorizer` sends requests to an external policy engine and delegates **wire format translation** to a pluggable `Provider` adapter. Each provider knows how to marshal kagent's `AuthzRequest` into the engine's expected format and unmarshal the engine's response back into an `AuthzDecision`.

```text
HTTP Request
     │
     ▼
AuthnMiddleware ──▶ session.Claims()
     │
     ▼
Handler.Check()
     │
     ▼
ExternalAuthorizer.Check()
     │
     ├── Provider.MarshalRequest(AuthzRequest)  → engine-specific JSON
     │
     ▼
HTTP POST to endpoint
     │
     ├── Provider.UnmarshalDecision(response)   → AuthzDecision
     ▼
AuthzDecision
```

When no external endpoint is configured, kagent falls back to the `NoopAuthorizer` which allows all requests.

## Provider Architecture

The **Provider** interface translates between kagent's internal types and engine-specific wire formats:

```go
type Provider interface {
    Name() string
    MarshalRequest(req auth.AuthzRequest) ([]byte, error)
    UnmarshalDecision(data []byte) (*auth.AuthzDecision, error)
}
```

### Built-in Providers

| Provider | Wire Format | When to Use |
|----------|-------------|-------------|
| **OPA** (default) | Request: `{"input": <AuthzRequest>}`, Response: `{"result": <AuthzDecision>}` | OPA's `/v1/data/` REST API |

### How Providers Work

The `ExternalAuthorizer` owns the HTTP transport (POST, status code checks, timeouts). The `Provider` owns the serialization:

1. `Provider.MarshalRequest()` wraps `AuthzRequest` into the engine's expected format
2. `ExternalAuthorizer` sends the HTTP POST
3. `Provider.UnmarshalDecision()` extracts `AuthzDecision` from the engine's response format

This separation means adding a new engine requires only a new `Provider` implementation — no changes to the HTTP transport layer.

## OPA Provider

The OPA provider is the default. It wraps requests for OPA's `/v1/data/` REST API.

### Request Format

OPA expects input wrapped in an `input` key:

```http
POST /v1/data/kagent/authz HTTP/1.1
Content-Type: application/json

{
  "input": {
    "claims": {
      "sub": "user-123",
      "email": "alice@example.com",
      "groups": ["platform-team"]
    },
    "resource": {
      "type": "Agent",
      "name": "default/my-agent"
    },
    "action": "get"
  }
}
```

### Response Format

OPA wraps policy output in a `result` key:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "result": {
    "allowed": true,
    "reason": ""
  }
}
```

### Field Reference

| Field | Type | Description |
|---|---|---|
| `input.claims` | `object \| null` | Session claims from the authentication layer (JWT claims, OIDC claims, etc.). `null` when no claims are available. |
| `input.resource.type` | `string` | Kubernetes resource kind (e.g. `Agent`, `Session`, `ModelConfig`) |
| `input.resource.name` | `string` | Resource identifier, typically `namespace/name` |
| `input.action` | `string` | One of: `get`, `create`, `update`, `delete` |
| `result.allowed` | `bool` | Whether the request is permitted |
| `result.reason` | `string` | Human-readable explanation (especially useful for denials) |

### Rego Policy Example

```rego
package kagent.authz

import rego.v1

default allowed := false
default reason := "no matching rule"

# OPA receives kagent's AuthzRequest inside `input`.
# The policy evaluates input.claims, input.resource, and input.action.

# Allow platform-team members full access.
allowed if {
    "platform-team" in input.claims.groups
}

reason := "" if { allowed }

# Allow agent-viewers read-only access to agents.
allowed if {
    "agent-viewers" in input.claims.groups
    input.resource.type == "Agent"
    input.action == "get"
}

# Deny deletion unless admin.
reason := "only admins can delete resources" if {
    input.action == "delete"
    not "admin" in input.claims.groups
}
```

### Error Handling

- **HTTP 200 with valid JSON** — Normal decision flow
- **Non-200 status** — Treated as a system error (not a denial)
- **Network error / timeout** — Treated as a system error

System errors are returned to the `Check()` helper, which implements **fail-open** semantics: the request is allowed and the error is logged.

## Configuration

### Environment Variables

```bash
# Required: URL of the authorization endpoint
EXTERNAL_AUTHZ_ENDPOINT=http://opa:8181/v1/data/kagent/authz

# Optional: provider type (defaults to "opa")
AUTHZ_PROVIDER=opa
```

When `EXTERNAL_AUTHZ_ENDPOINT` is empty or unset, kagent uses the `NoopAuthorizer` (all requests allowed). `AUTHZ_PROVIDER` is only used when an endpoint is set.

### CLI Flags

```bash
--external-authz-endpoint=http://opa:8181/v1/data/kagent/authz
--authz-provider=opa
```

### Helm Values

```yaml
controller:
  authorization:
    provider: "opa"  # or "" (defaults to opa)
    externalEndpoint: "http://opa:8181/v1/data/kagent/authz"
```

This sets the `EXTERNAL_AUTHZ_ENDPOINT` and `AUTHZ_PROVIDER` environment variables in the controller configmap.

## Adding a New Provider

To add support for a new policy engine (e.g. Cerbos):

1. **Create the provider file**: `go/internal/httpserver/auth/provider_cerbos.go`

   ```go
   package auth

   type CerbosProvider struct{}

   func (p *CerbosProvider) Name() string { return "cerbos" }

   func (p *CerbosProvider) MarshalRequest(req auth.AuthzRequest) ([]byte, error) {
       // Translate AuthzRequest to Cerbos CheckResourcesRequest format
   }

   func (p *CerbosProvider) UnmarshalDecision(data []byte) (*auth.AuthzDecision, error) {
       // Translate Cerbos CheckResourcesResponse to AuthzDecision
   }
   ```

2. **Register in the factory** (`provider.go`):

   ```go
   func ProviderByName(name string) (Provider, error) {
       switch name {
       case "opa", "":
           return &OPAProvider{}, nil
       case "cerbos":
           return &CerbosProvider{}, nil
       default:
           return nil, fmt.Errorf("unknown authz provider: %q (supported: opa, cerbos)", name)
       }
   }
   ```

3. **Add tests**: `go/internal/httpserver/auth/provider_test.go` — add table entries for the new provider's marshal/unmarshal behavior.

4. **Update docs**: Add the new provider to the table in this document.

## Current Authorization Architecture

### The `Authorizer` interface

All authorization flows through one interface defined in `go/pkg/auth/auth.go`:

```go
type Authorizer interface {
    Check(ctx context.Context, req AuthzRequest) (*AuthzDecision, error)
}
```

### The `Check()` helper

Handlers never call the `Authorizer` directly. They use a helper in `go/internal/httpserver/handlers/helpers.go` that:

1. Maps the HTTP method to an `auth.Verb` (`GET` → `get`, `POST` → `create`, etc.)
2. Extracts `Claims` from the request context (set by the authentication middleware)
3. Calls `authorizer.Check()`
4. Implements **fail-open** semantics: if `Check()` returns an error, the request is allowed and the error is logged

Every handler follows the same pattern:

```go
if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent"}); err != nil {
    w.RespondWithError(err)
    return
}
```

### The `UnsecureAuthenticator`

The current authenticator builds a session with **no claims**. A real JWT/OIDC authenticator would populate the claims map, which flows automatically through the pipeline:

```
AuthnMiddleware → context → Check() helper → AuthzRequest.Claims → ExternalAuthorizer
```

## Fail-Open vs Fail-Closed

### Current behavior (fail-open)

When `authorizer.Check()` returns an error (e.g. the external endpoint is unreachable), the `Check()` helper logs the error and **allows the request**:

```go
if err != nil {
    log.Error(err, "authorization check failed, allowing access (fail-open)")
    return nil
}
```

### Switching to fail-closed

For production, change the error handling in `Check()` to deny on error:

```go
if err != nil {
    log.Error(err, "authorization check failed, denying access (fail-closed)")
    return errors.NewServiceUnavailableError(
        "authorization service unavailable",
        fmt.Errorf("authz check failed: %w", err),
    )
}
```

### Recommended rollout strategy

1. **Audit mode** — Deploy with fail-open. Log decisions but never block. Monitor for unexpected denials.
2. **Shadow mode** — Run the external authorizer alongside `NoopAuthorizer`. Compare results.
3. **Enforce mode** — Switch to fail-closed once policy coverage is validated.

## What Does NOT Change

| Component | Why it stays the same |
|---|---|
| `handlers/*.go` call sites | They call `Check(h.Authorizer, r, resource)` — the authorizer is injected |
| `Check()` helper in `handlers/helpers.go` | It maps HTTP methods, extracts claims, and delegates — all generic |
| `AuthzRequest` / `AuthzDecision` types | They carry claims, resource, action, and allowed/reason |
| `Session` interface | It already exposes `Claims() map[string]any` |
| `AuthnMiddleware` | It stores the session in context regardless of authenticator implementation |
| `ExtensionConfig` / `ServerConfig` | They accept `auth.Authorizer` (interface), not a concrete type |

## Related Files

- [go/pkg/auth/auth.go](../../go/pkg/auth/auth.go) — `Authorizer` interface, `AuthzRequest`, `AuthzDecision`, `Session` interface, `AuthnMiddleware`
- [go/internal/httpserver/auth/authz.go](../../go/internal/httpserver/auth/authz.go) — `NoopAuthorizer` implementation
- [go/internal/httpserver/auth/external_authz.go](../../go/internal/httpserver/auth/external_authz.go) — `ExternalAuthorizer` implementation
- [go/internal/httpserver/auth/provider.go](../../go/internal/httpserver/auth/provider.go) — `Provider` interface and `ProviderByName` factory
- [go/internal/httpserver/auth/provider_opa.go](../../go/internal/httpserver/auth/provider_opa.go) — OPA provider implementation
- [go/internal/httpserver/auth/authn.go](../../go/internal/httpserver/auth/authn.go) — `UnsecureAuthenticator`, `SimpleSession`, `A2AAuthenticator`
- [go/internal/httpserver/handlers/helpers.go](../../go/internal/httpserver/handlers/helpers.go) — `Check()` helper with fail-open logic
- [go/cmd/controller/main.go](../../go/cmd/controller/main.go) — Authorizer wiring point
- [go/pkg/app/app.go](../../go/pkg/app/app.go) — `ExtensionConfig` that carries the `Authorizer` to the HTTP server
- [go/pkg/auth/external_authz_test.go](../../go/pkg/auth/external_authz_test.go) — Tests for the `Authorizer` interface contract
- [go/internal/httpserver/auth/external_authz_test.go](../../go/internal/httpserver/auth/external_authz_test.go) — Tests for the `ExternalAuthorizer` implementation
- [go/internal/httpserver/auth/provider_test.go](../../go/internal/httpserver/auth/provider_test.go) — Tests for provider implementations
