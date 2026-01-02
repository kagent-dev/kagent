# EP-476: Enterprise Enablement

* Issue: [#476](https://github.com/kagent-dev/kagent/issues/476)

## Background

This EP addresses enterprise deployment requirements for kagent, specifically authentication, multi-tenancy, and audit logging. These features are prerequisites for production deployment in regulated environments.

## Motivation

Currently kagent has limited support for enterprise deployment scenarios:

1. **Authentication**: Only `UnsecureAuthenticator` is implemented
2. **Multi-tenancy**: Controller operates cluster-wide with no namespace isolation
3. **Audit logging**: Basic HTTP logging without compliance-ready audit trail

### Goals

1. Implement OAuth2/OIDC authentication provider
2. Add namespace-scoped controller mode for multi-tenancy
3. Add structured audit logging for compliance

### Non-Goals

- RBAC authorization (future work)
- Multi-cluster support (separate EP)
- Air-gapped installation (documentation only)

## Implementation Details

### OAuth2/OIDC Authentication

New `OAuth2Authenticator` implementing `auth.AuthProvider`:

```go
// go/internal/httpserver/auth/oauth2.go
type OAuth2Config struct {
    IssuerURL        string
    ClientID         string
    Audience         string
    RequiredScopes   []string
    UserIDClaim      string   // default: "sub"
    RolesClaim       string   // default: "roles"
}
```

Features:
- JWT validation with JWKS caching
- Configurable claims extraction
- Scope and audience validation
- Bearer token from header or query parameter

### Namespace-Scoped Controller

Add `watchedNamespaces` parameter to reconciler:

```go
// go/internal/controller/reconciler/reconciler.go
type kagentReconciler struct {
    // If empty, cluster-wide. If set, only these namespaces.
    watchedNamespaces []string
}

func (a *kagentReconciler) validateNamespaceIsolation(namespace string) error
```

All reconcile methods call `validateNamespaceIsolation()` before processing.

### Structured Audit Logging

Middleware that logs compliance-ready JSON:

```go
// go/internal/httpserver/middleware.go
type AuditLogConfig struct {
    Enabled        bool
    LogLevel       int
    IncludeHeaders []string
}
```

Logged fields: `request_id`, `timestamp`, `user`, `user_roles`, `namespace`, `action`, `status`, `duration_ms`

### Helm Configuration

```yaml
# values.yaml
controller:
  watchNamespaces: []  # empty = cluster-wide

pdb:
  enabled: false
  controller:
    minAvailable: 1

metrics:
  serviceMonitor:
    enabled: false
```

### Test Plan

- Unit tests for OAuth2 token validation (8 test cases)
- Unit tests for namespace isolation (15 test cases)
- Unit tests for audit middleware (11 test cases)
- Integration tests with mock OIDC server

## Alternatives

1. **Use existing auth middleware**: Rejected - kagent needs session-based auth for A2A protocol
2. **Namespace isolation via RBAC only**: Rejected - controller still needs to enforce boundaries
3. **External audit logging**: Considered - middleware approach is simpler and integrates with existing logging

## Open Questions

1. Should OAuth2 config be a CRD or Helm values? (Currently Helm values)
2. Integration with OpenShift OAuth server? (Future work)
