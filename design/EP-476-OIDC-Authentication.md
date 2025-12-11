# EP-476: OIDC Authentication Integration

* Issue: [#476](https://github.com/kagent-dev/kagent/issues/476)

## Background

KAgent currently uses an unsecure authentication mechanism (`UnsecureAuthenticator`) that accepts any user ID provided via query parameters or headers, with no validation. This development-grade authentication is suitable only for trusted environments and poses significant security risks for production deployments.

This proposal adds enterprise-grade authentication to KAgent by implementing a standard OIDC (OpenID Connect) client that works with any compliant OIDC provider. This enables integration with enterprise identity systems including Keycloak, Auth0, Okta, Azure AD, Google, or Dex. KAgent will not bundle an identity provider; instead, users deploy and manage their OIDC provider separately, following infrastructure separation principles.

**Sponsors**: Collin Walker (@lets-call-n-walk)

**Origin**: Security hardening initiative to enable production deployments

## Motivation

### Current State
- **No real authentication**: System trusts any user ID provided by the client
- **No user management**: Users are just string identifiers with no profiles or credentials
- **Client-side identity**: User ID stored in browser localStorage can be trivially modified
- **No authorization**: NoopAuthorizer allows all users to perform all operations
- **Impersonation vulnerability**: Anyone can impersonate any user by changing query parameters
- **Not production-ready**: Suitable only for development/testing environments

### Why OIDC with External Provider?
1. **Standards-based**: OIDC is an industry-standard protocol supported by all major identity providers
2. **Enterprise integration**: Works with existing identity infrastructure (Okta, Azure AD, Keycloak, Auth0, Google, etc.)
3. **Flexibility**: Organizations choose their preferred identity provider based on requirements
4. **Separation of concerns**: KAgent focuses on agent orchestration, not identity management
5. **Scalability**: External providers can be deployed with HA and persistent storage
6. **Multi-backend support**: Providers like Dex aggregate multiple identity sources (LDAP, SAML, GitHub, etc.) through a single OIDC interface
7. **Operational simplicity**: No identity provider lifecycle management within KAgent deployments

### Goals

1. **Add secure authentication** using OAuth2/OIDC flow with proper token-based authentication
2. **Support any OIDC provider** via standard OIDC discovery and token validation
3. **Work with existing identity infrastructure** including Keycloak, Auth0, Okta, Azure AD, Google, Dex, and other OIDC-compliant providers
4. **Implement session management** with secure token storage, refresh, and revocation
5. **Add RBAC foundation** with group-based role mapping from OIDC claims
6. **Maintain backward compatibility** during migration with feature flags
7. **Provide migration path** from unsecure auth to OIDC with clear documentation
8. **Support both UI and CLI** authentication flows (web flow for UI, device code or browser flow for CLI)

### Non-Goals

1. **Deploying or managing identity providers**: Users must deploy and manage their own OIDC provider (Dex, Keycloak, etc.)
2. **Identity provider configuration**: KAgent does not configure upstream identity providers; that's the operator's responsibility
3. **Custom user database**: Users will be managed by external identity providers, not stored locally
4. **Fine-grained permissions**: Initial implementation focuses on authentication; detailed RBAC policies come in future iterations
5. **Multi-tenancy isolation**: Organization/workspace-level isolation is out of scope
6. **User provisioning API**: No self-service user registration or profile management
7. **Social login widgets**: Only identity providers configured by administrators are supported
8. **Advanced token encryption**: Basic token protection will be implemented; advanced features like HSM integration or key rotation are deferred

## Implementation Details

### Architecture Overview

**Component Structure** (External OIDC Provider):

```
┌─────────────────────────────────────────────────────────────┐
│                         User Browser                         │
└────┬──────────────────────────────────────────────────┬─────┘
     │                                                    │
     │ 1. /auth/login                                    │ 4. Set cookie
     │                                                    │    with JWT
     ▼                                                    │
┌─────────────────────────────────────────────────────────────┐
│               kagent-server (Go HTTP Server)                │
│                                                              │
│  • /auth/login    → Redirect to OIDC OAuth2 authorize      │
│  • /auth/callback → Exchange code for tokens               │
│  • /auth/logout   → Revoke tokens, clear session           │
│  • /api/*         → Protected API endpoints (authN/authZ)  │
│                                                              │
│  Middleware:                                                │
│    - AuthnMiddleware → Verify JWT, refresh if needed       │
│    - AuthzMiddleware → Check RBAC policies                 │
│                                                              │
│  Configuration:                                             │
│    - OIDC Issuer URL (--oidc-issuer-url)                   │
│    - Client ID/Secret (ConfigMap/Secret)                   │
│    - Scopes, claims, RBAC policies                         │
└────┬────────────────────────────────────────────────────┬───┘
     │                                                     │
     │ 2. OAuth2 authorize                                │ 3. ID token
     │                                                     │
     ▼                                                     │
┌──────────────────────────────────────────────────────────────┐
│        External OIDC Provider (User-Deployed)                │
│                                                              │
│  Examples:                                                   │
│    - Keycloak (self-hosted, full-featured)                  │
│    - Dex (lightweight, multi-connector aggregator)          │
│    - Auth0 (SaaS, easy setup)                               │
│    - Okta (enterprise SaaS)                                 │
│    - Azure AD / Entra ID (Microsoft cloud)                  │
│    - Google Workspace                                       │
│                                                              │
│  Configuration:                                             │
│    - Static client: kagent (with redirect URIs)            │
│    - User/group management                                  │
│    - Backend connectors (optional: LDAP, SAML, etc.)       │
└──────────────────────────────────────────────────────────────┘
```

### Design Decision: External OIDC Provider vs Bundled

ArgoCD bundles Dex as a sidecar deployment. KAgent takes a different approach based on lessons learned:

**Why KAgent Uses External OIDC Provider:**

| Aspect | ArgoCD (Bundled Dex) | KAgent (External OIDC) |
|--------|----------------------|------------------------|
| **Provider Choice** | Dex only | Any OIDC provider (Keycloak, Okta, Dex, Auth0, etc.) |
| **Scalability** | Single replica only (in-memory storage) | Provider-dependent (most support HA) |
| **HA Support** | Not available - sessions lost on restart | Supported by most enterprise providers |
| **Resource Usage** | Each app needs own Dex instance | Shared provider across multiple services |
| **Maintenance** | Coupled to app release cycle | Independent lifecycle management |
| **Complexity** | Wrapper code, config generation, reverse proxy | Standard OIDC client only |
| **Enterprise Fit** | Cannot leverage existing providers | Reuses existing infrastructure |

**Trade-off**: External OIDC requires users to deploy and manage their identity provider separately, but provides flexibility, scalability, and follows infrastructure separation principles.

### Proposed Implementation for KAgent

KAgent will implement a standard OIDC client that works with any compliant OIDC provider:

#### Phase 1: Core OIDC Authentication (MVP)

**1. OIDC Client Implementation** (Go)
- Package: `go/internal/httpserver/auth/oidc/`
- `OIDCAuthenticator` implements `AuthProvider` interface
- Uses coreos/go-oidc library for OIDC discovery and verification
- Methods:
  - `HandleLogin()` - Initiates OAuth2 flow with PKCE
  - `HandleCallback()` - Exchanges code for tokens, sets secure cookies
  - `Authenticate()` - Verifies JWT from cookies, extracts claims
  - `RefreshToken()` - Uses refresh token to get new access/id tokens
- Configuration (via flags or environment variables):
  - `--oidc-issuer-url` - OIDC provider URL (e.g., `https://dex.example.com`)
  - `--oidc-client-id` - OAuth2 client ID
  - `--oidc-client-secret` - OAuth2 client secret (direct value or k8s secret reference)
  - `--oidc-scopes` - Comma-separated scopes (default: `openid,profile,email,groups`)
  - `--oidc-group-claim` - JWT claim path for groups (default: `groups`)

**2. Session Management**

**Storage Options**:

**Option A: Database-backed (PostgreSQL/SQLite)** - Recommended for MVP
- Leverage existing database connection (no new infrastructure)
- Store tokens/sessions in new tables (encrypted at rest)
- Good for: Single-cluster deployments, simplified operations
- Trade-off: Slightly slower (~10-50ms)

**Option B: In-Memory (Process Memory)** - Simplest
- Store sessions in Go map with mutex protection
- Good for: Development, single-replica deployments
- Trade-off: Sessions lost on pod restart, no multi-replica support

**Option C: Redis** - Best for scale (requires new infrastructure)
- Dedicated caching layer for tokens
- Good for: Multi-replica deployments, high performance
- Trade-off: Adds operational complexity (new service to manage)

**Chosen for Phase 1: Option A (Database-backed)**
- **Token Protection**: Tokens encrypted at rest using AES-256-GCM
- **Token Lifecycle**:
  - ID tokens cached in `auth_tokens` table with 5-minute expiration
  - Refresh tokens stored encrypted for token renewal
  - Revoked tokens tracked in `revoked_tokens` table until expiration
  - Automatic cleanup of expired entries via cron job
- **HTTP Sessions**: Session state stored in httpOnly cookies (JWT) + database cache
  - Cookie contains ID token JWT (split into 4KB chunks if needed)
  - Database caches decrypted claims and refresh tokens keyed by session ID
  - Session ID derived from JWT's `jti` claim
- **Multi-Replica Support**: Database provides shared state across replicas
- Session struct extended with JWT claims (sub, groups, email, etc.)

**3. Authentication Endpoints**
```
GET  /auth/login           - Redirect to OIDC provider's OAuth2 authorize endpoint
GET  /auth/callback        - Handle OAuth2 callback, exchange code for tokens
POST /auth/logout          - Revoke tokens, clear cookies, redirect to logout URL
GET  /auth/userinfo        - Return current user info (from JWT claims)
```

**4. Frontend Integration** (TypeScript/React)
- Update API client (`ui/src/app/actions/utils.ts`)
  - Remove `user_id` query parameter
  - Use cookie-based authentication (httpOnly cookies)
- Add login page component
  - Display available identity providers from backend config
  - Redirect to `/auth/login?return_url=<current-page>`
- Add logout button
  - Call `/auth/logout` endpoint
- Handle 401 responses
  - Redirect to login page with return URL

**5. CLI Authentication Flow**
- Device code flow for CLI authentication:
  ```bash
  kagent login
  # Output: Visit https://kagent.example.com/auth/device?code=XXXX
  # CLI polls for completion
  # Stores token in ~/.kagent/token
  ```
- Support `KAGENT_TOKEN` environment variable
- Add `--token` flag to override file/env

**6. RBAC Foundation**
- Create `kagent-rbac-cm` ConfigMap
- Support Casbin-style policies:
  ```csv
  p, role:admin, agents, *, *, allow
  p, role:user, agents, get, *, allow
  g, admin@example.com, role:admin
  g, my-org:developers, role:user
  ```
- Extract groups from OIDC claims (configurable via `--oidc-group-claim`)
- Add `RBACAuthorizer` implementing `Authorizer` interface

**Group Claim Handling** (Provider-Specific):

Different OIDC providers return groups in different formats. RBAC policies must match the provider's group format:

| Provider | Group Format | Example | RBAC Policy Example |
|----------|--------------|---------|---------------------|
| **Keycloak** | Path-based | `/kagent-admins` | `g, /kagent-admins, role:admin` |
| **Dex (GitHub)** | org:team | `myorg:engineering` | `g, myorg:engineering, role:admin` |
| **Dex (LDAP)** | DN or CN | `cn=admins,ou=groups` | `g, cn=admins\,ou=groups, role:admin` |
| **Okta** | Group names | `Developers` | `g, Developers, role:admin` |
| **Azure AD** | Object IDs | `84ce98d1-e359-...` | `g, 84ce98d1-e359-..., role:admin` |
| **Google** | Email-based | `devs@example.com` | `g, devs@example.com, role:admin` |

**Configuration Requirements**:
- `--oidc-group-claim`: Specifies which JWT claim contains groups (default: `groups`)
  - Some providers use `roles`, `memberOf`, or custom claims
- Operators must understand their provider's group format when writing RBAC policies
- No automatic group normalization in Phase 1 (groups used as-is from JWT)

**7. Migration Strategy**
- Add `--auth-enabled` flag (default: false for backward compatibility)
- When disabled, use `UnsecureAuthenticator`
- When enabled, use `OIDCAuthenticator`

**Migration Steps** (for existing deployments):

1. **Prerequisites**:
   - Deploy external OIDC provider (Keycloak, Dex, etc.) and verify accessibility
   - Configure DNS/Ingress for both KAgent and OIDC provider
   - Register KAgent as a client in the OIDC provider with appropriate redirect URIs
   - Ensure TLS certificates are installed
   - Run database migrations to create auth tables

2. **Configuration**:
   - OIDC settings initially handled in feature flags, as is current pattern (issuer, client-id, scopes) - add configmap in future
   - Create `kagent-secret` Secret with client-secret
   - Create `kagent-rbac-cm` ConfigMap with initial RBAC policies
   - Map OIDC `sub` claims to existing user_id values if needed

3. **Enablement**:
   - Set `--auth-enabled=true` in kagent-server deployment
   - Restart kagent-server pods
   - **All existing sessions are immediately invalidated**
   - All API requests with `user_id` query parameters will be rejected with 401

4. **User Communication**:
   - Notify users that authentication is being enabled
   - Provide login instructions for the new OIDC flow
   - Share RBAC policy details so users know their permissions

5. **Verification**:
   - Test login flow via UI (should redirect to Dex)
   - Test CLI authentication (`kagent login`)
   - Verify RBAC policies are enforced correctly
   - Check audit logs for any authentication failures

6. **Rollback** (if needed):
   - Set `--auth-enabled=false`
   - Restart kagent-server
   - Users can immediately access with unsecure auth (user_id query params)
   - No data loss; only affects authentication layer

#### Phase 2: Enhanced Security & Features

**1. Advanced RBAC**
- Fine-grained resource permissions
- Namespace/project-level isolation
- User-specific resource filtering in database queries
- Audit logging for authorization decisions

**2. CLI Enhancements**
- Browser-based auth with local callback server
- Token refresh in CLI
- Multiple profile support

**3. Observability**
- Prometheus metrics for auth operations (login, logout, token refresh, failures)
- OpenTelemetry tracing for auth flow
- Structured logging for debugging

### Configuration Examples

**Example 1: Keycloak**

First, configure Keycloak (deployed separately):
- Create realm: `kagent`
- Create client: `kagent-client` with redirect URI `https://kagent.example.com/auth/callback`
- Enable group claims in client mappers
- Create groups and assign users

Then, configure KAgent:

`kagent` Feature Flags/Env Variables:
```
  - `--oidc-issuer-url` - OIDC provider URL\
  - `--oidc-client-id` - OAuth2 client ID
  - `--oidc-client-secret` - OAuth2 client secret (direct value or k8s secret reference)
  - `--oidc-scopes` - Comma-separated scopes (default: `openid,profile,email,groups`)
  - `--oidc-group-claim` - JWT claim path for groups (default: `groups`)
```

`kagent-secret` Secret:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kagent-secret
  namespace: kagent
type: Opaque
stringData:
  oidc.client-secret: <replace-with-client-secret>
```

`kagent-rbac-cm` ConfigMap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kagent-rbac-cm
  namespace: kagent
data:
  policy.default: role:readonly
  policy.csv: |
    p, role:admin, *, *, *, allow
    p, role:readonly, agents, get, *, allow
    p, role:readonly, sessions, get, *, allow
    g, /kagent-admins, role:admin
    g, /kagent-users, role:readonly
  scopes: '[groups, email]'
```

**Example 2: Dex with GitHub Backend**

First, configure Dex (deployed separately) with `dex-config.yaml`:
```yaml
issuer: https://dex.example.com
storage:
  type: kubernetes
  config:
    inCluster: true
connectors:
- type: github
  id: github
  name: GitHub
  config:
    clientID: <github-oauth-app-client-id>
    clientSecret: <github-oauth-app-client-secret>
    redirectURI: https://dex.example.com/callback
    orgs:
    - name: your-org
staticClients:
- id: kagent
  name: KAgent
  secret: <shared-secret>
  redirectURIs:
  - 'https://kagent.example.com/auth/callback'
```

Then configure KAgent:
```
  - --oidc-issuer-url https://dex.example.com
  - --oidc-client-id kagent
  - --oidc-scopes "openid,profile,email,groups"
  - --oidc-group-claim groups
```

**Example 3: Okta**

```yaml
  - --oidc-issuer-url https://dev-123456.okta.com/oauth2/default
  - --oidc-client-id <okta-application-client-id>
  - --oidc-scopes "openid,profile,email,groups"
  - --oidc-group-claim groups
```

**Example 4: Command-line Flags**

```bash
kagent-server \
  --auth-enabled=true \
  --oidc-issuer-url=https://keycloak.example.com/realms/kagent \
  --oidc-client-id=kagent-client \
  --oidc-client-secret=<client-secret> \
  --oidc-scopes=openid,profile,email,groups \
  --oidc-group-claim=groups
```

### Database Changes

**New Tables:**

```sql
-- Token cache (for refresh tokens and ID token claims)
CREATE TABLE auth_tokens (
  session_id VARCHAR(255) PRIMARY KEY,
  user_id VARCHAR(255) NOT NULL,
  encrypted_token BYTEA NOT NULL,  -- AES-256-GCM encrypted oauth2.Token
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  INDEX idx_user_id (user_id),
  INDEX idx_expires_at (expires_at)
);

-- Revoked tokens (for logout)
CREATE TABLE revoked_tokens (
  jti VARCHAR(255) PRIMARY KEY,  -- JWT ID claim
  user_id VARCHAR(255) NOT NULL,
  revoked_at TIMESTAMP NOT NULL,
  expires_at TIMESTAMP NOT NULL,  -- When to purge from table
  INDEX idx_expires_at (expires_at),
  INDEX idx_user_id (user_id)
);

-- Optional: Audit log for auth events
CREATE TABLE auth_audit_log (
  id VARCHAR(255) PRIMARY KEY,
  user_id VARCHAR(255) NOT NULL,
  event_type VARCHAR(50) NOT NULL, -- login, logout, token_refresh, auth_failure
  timestamp TIMESTAMP NOT NULL,
  ip_address VARCHAR(45),
  user_agent TEXT,
  success BOOLEAN NOT NULL,
  error_message TEXT,
  INDEX idx_user_timestamp (user_id, timestamp),
  INDEX idx_timestamp (timestamp)
);
```

**Background Cleanup Job:**
- Periodically delete expired entries from `auth_tokens` and `revoked_tokens`
- Run as cron job or background goroutine every 1 hour
- Query: `DELETE FROM auth_tokens WHERE expires_at < NOW()`
- Query: `DELETE FROM revoked_tokens WHERE expires_at < NOW()`

**Schema Migrations:**
- No changes to existing tables required
- User ID field in existing tables remains string-based
- OIDC `sub` claim maps to existing `user_id` fields

### API Changes

**New Endpoints:**
```
GET  /auth/login          - Initiate OIDC login
GET  /auth/callback       - OAuth2 callback handler
POST /auth/logout         - Logout and revoke tokens
GET  /auth/userinfo       - Get current user info
GET  /auth/providers      - List configured identity providers
```

**Modified Endpoints:**
- All `/api/*` endpoints: Remove support for `user_id` query parameter when auth is enabled
- Return 401 Unauthorized if no valid JWT cookie present
- Extract user ID from JWT claims instead of query params

**CLI Commands:**
```bash
kagent login                  # Authenticate CLI
kagent logout                 # Clear stored credentials
kagent whoami                 # Show current user
```

### Dependencies

**New Go Modules:**
- `github.com/coreos/go-oidc/v3` - OIDC client library
- `golang.org/x/oauth2` - OAuth2 client (already used by go-oidc)
- `github.com/casbin/casbin/v2` - RBAC policy engine
- Existing: `gorm.io/gorm` - Already used; will add auth tables

**External Infrastructure (User-Provided):**
- **OIDC Provider**: Users choose and deploy their preferred provider
  - Examples: Keycloak, Dex, Auth0, Okta, Azure AD, Google Workspace
  - Requirements: Must support standard OIDC discovery (`.well-known/openid-configuration`)
  - Recommended: HA deployment with persistent storage for production
- **Database**: Uses existing database (PostgreSQL or SQLite)
  - No new infrastructure required
  - Three new tables added (auth_tokens, revoked_tokens, auth_audit_log)

**Optional for Local Development:**
- `quay.io/keycloak/keycloak:latest` - Full-featured OIDC provider
- `ghcr.io/dexidp/dex:v2.43.0` - Lightweight OIDC aggregator
- `ghcr.io/ory/hydra:latest` - OAuth2/OIDC server

### Security Considerations

1. **Token Storage**
   - Encrypt tokens at rest using AES-256-GCM
   - Use httpOnly, Secure, SameSite cookies for web clients
   - Store CLI tokens in `~/.kagent/` with 0600 permissions

2. **CSRF Protection**
   - State parameter with encrypted nonce in OAuth2 flow
   - SameSite=Lax cookie attribute
   - Validate returnURL against allowlist

3. **TLS Requirements**
   - Require HTTPS in production (enforced by Secure cookie flag)
   - Support custom CA certificates for Dex
   - Option to disable TLS verification for development

4. **Rate Limiting**
   - Implement login attempt rate limiting (5 failures per 5 minutes)
   - Exponential backoff on failures
   - Per-username tracking

5. **Audit Logging**
   - Log all authentication events (success/failure)
   - Include timestamp, user, IP, user-agent
   - Store in database for compliance

### Test Plan

**Unit Tests:**
- OIDC authenticator: token verification, refresh, revocation
- RBAC authorizer: policy evaluation, group mapping
- Session manager: token encryption, storage, retrieval
- Config generator: Dex config generation with secret substitution

**Integration Tests:**
- End-to-end OAuth2 flow with mock OIDC provider
- Token refresh flow
- Logout and revocation
- RBAC policy enforcement across API endpoints
- Configuration hot-reload

**E2E Tests:**
- Deploy test Dex instance with in-memory storage
- Login via mock GitHub connector (using test organization)
- Login via generic OIDC connector (Keycloak or ORY Hydra)
- CLI device code flow
- Session persistence across requests
- Token expiration and refresh
- Multi-replica kagent-server
- External Dex instance restart (verify KAgent recovers gracefully)

**Security Tests:**
- CSRF attack prevention (invalid state parameter)
- Token replay attack (revoked tokens)
- Authorization bypass attempts
- SQL injection via user ID
- XSS via return URL parameter

### Performance Considerations

- Token verification: ~1ms (estimated: JWT signature check + cache lookup)
- OIDC discovery: Cached, refresh every 5 minutes
- Authorization check: ~0.5ms (estimated: in-memory Casbin policy)
- Token refresh: ~50-200ms (network call to Dex/upstream IdP)
- Impact: Adds ~2-5ms estimated latency to authenticated requests

### Error Handling

**1. Dex/OIDC Provider Unavailable**:
- Return 503 Service Unavailable with `Retry-After` header
- Cache OIDC discovery metadata for up to 1 hour to handle temporary outages
- Implement circuit breaker pattern (open after 5 consecutive failures, half-open after 30s)
- Log error with correlation ID for troubleshooting
- Frontend: Display friendly error message with retry option

**2. Token Expired During Request**:
- Attempt automatic refresh using cached refresh token
- If refresh succeeds: Set new JWT cookie and continue processing request
- If refresh fails (invalid refresh token, expired, or network error):
  - Return 401 Unauthorized with `X-Auth-Required: true` header
  - Frontend: Redirect to `/auth/login?return_url=<current-url>`
  - CLI: Display login instructions and exit with code 1

**3. Invalid JWT Signature**:
- Clear all auth cookies immediately
- Return 401 Unauthorized with error message
- Log as potential security issue (modified token, wrong signing key, clock skew)
- Increment `auth_invalid_token_total` metric for monitoring
- Check for clock skew: allow 5-minute leeway for `iat` and `exp` claims

**4. Missing or Invalid Claims**:
- JWT valid but missing required claims (sub, email, groups):
  - Return 401 with specific error message
  - Log issue for operator investigation (misconfigured Dex connector)
- Invalid claim format (groups not array, sub not string):
  - Reject authentication
  - Return 422 Unprocessable Entity

**5. Network Failures (Dex Communication)**:
- Implement retry with exponential backoff (3 attempts: 100ms, 500ms, 2s)
- Timeout configuration: 10s for OIDC discovery, 5s for token exchange
- After all retries exhausted: Return 503 with retry guidance
- Preserve user context where possible (cached claims for read-only operations)

**6. RBAC Authorization Failure**:
- Return 403 Forbidden with descriptive message
- Log authorization decision (user, resource, action, policy result)
- Include which RBAC rule caused denial (if DEBUG logging enabled)
- Frontend: Display permission denied message with contact information

**7. Database Connection Failure**:
- Authentication fails gracefully: Return 503 Service Unavailable
- Token verification from cookies still works (JWT self-contained, doesn't need DB)
- Warning: Token revocation checking and refresh will be unavailable
- Log error and emit metric for alerting
- Operators should monitor database health
- Consider: Allow read-only operations with stale token data

**8. Session Revocation (Logout)**:
- Best-effort revocation: Add token `jti` to `revoked_tokens` table
- If database unavailable: Token remains valid until expiration (up to 24h)
- Log revocation attempt and database status
- Return success to user even if database write fails (UX over perfect security)
- Token verification middleware checks revocation table on each request

**9. OIDC Discovery Failures**:
- On startup: Fail fast if discovery fails (prevent misconfiguration)
- During runtime: Use cached discovery for up to 1 hour
- Refresh discovery in background every 5 minutes
- If refresh fails 3 times consecutively: Log warning but continue with cached config

**10. Concurrent Login/Logout Races**:
- Use JWT `jti` (token ID) for revocation checks before `sub` lookup
- Database transactions ensure atomic operations
- Use `INSERT ... ON CONFLICT` for upsert operations
- Last-write-wins for conflicting sessions (newer token replaces older)

### Documentation Requirements

1. **Operator Manual**
   - **Choosing an OIDC Provider**: Comparison guide
     - Keycloak vs Dex vs Auth0 vs Okta: feature comparison
     - Self-hosted vs SaaS considerations
     - Multi-backend requirements (LDAP, SAML support)
   - **Provider-Specific Setup Guides**:
     - Keycloak: Realm setup, client configuration, group mappers
     - Dex: Deployment, connectors, static clients
     - Okta: Application setup, group claims
     - Azure AD: App registration, API permissions
   - **Configuring KAgent**: OIDC integration setup
     - ConfigMap/Secret configuration
     - Command-line flags reference
     - TLS/certificate considerations
   - **RBAC Setup**: Policy syntax, examples, and best practices
     - **Critical**: Provider-specific group format documentation
     - Examples showing how to write policies for each provider's group format
     - How to discover group format from your provider (inspect JWT claims)
   - **Troubleshooting**: Common auth issues and debugging
     - Group claim mismatches (RBAC not matching provider format)
     - Missing groups in ID token (need UserInfo endpoint)

2. **User Guide**
   - How to log in via UI
   - CLI authentication setup (`kagent login`)
   - Managing multiple contexts
   - Understanding roles and permissions

3. **Developer Guide**
   - Architecture overview (OIDC flow diagram)
   - Local development setup (using Keycloak or Dex in docker-compose)
   - Custom authorizer implementation
   - Testing auth flows with mock OIDC providers (ORY Hydra, mock-oauth2-server)

## Alternatives

### Alternative 1: Bundled Identity Provider (Like ArgoCD)
- **Description**: Bundle an identity provider (Dex) as a sidecar deployment with KAgent
- **Pros**: Simpler initial setup, no external dependencies, single helm chart
- **Cons**: Cannot scale horizontally, single replica limitation, tight coupling, resource duplication across services, limits provider choice
- **Verdict**: Rejected - External approach provides better scalability, flexibility, and separation of concerns

### Alternative 2: No Standards-Based Auth (Custom Protocol)
- **Description**: Build custom authentication protocol instead of using OIDC
- **Pros**: Full control over authentication flow
- **Cons**: Reinventing the wheel, poor enterprise integration, no existing tooling support, security risks
- **Verdict**: Rejected - OIDC is the industry standard with proven security

### Alternative 3: OAuth2 Proxy
- **Description**: Use oauth2-proxy as external authentication service
- **Pros**: Battle-tested, supports many providers
- **Cons**: No group claim aggregation from multiple sources, less customizable, no LDAP/SAML native support
- **Verdict**: Rejected - Dex provides better multi-backend support and ArgoCD precedent

### Alternative 4: Service Mesh Authentication (Istio/Linkerd)
- **Description**: Delegate authentication entirely to service mesh layer
- **Pros**: Centralized policy management, no application code changes
- **Cons**: Requires service mesh installation, limited to Kubernetes environments, doesn't solve CLI authentication
- **Verdict**: Rejected - Too much infrastructure dependency, not portable to non-Kubernetes environments

### Alternative 5: API Keys Only
- **Description**: Simple API key authentication instead of OIDC
- **Pros**: Very simple, no OAuth2 complexity
- **Cons**: No SSO, no group management, manual key distribution, no user context
- **Verdict**: Rejected for primary auth - May add as supplementary machine-to-machine auth in Phase 2

## Open Questions

1. **Token Storage Backend** ✅ RESOLVED
   - Should we use Redis, database, or in-memory storage?
   - Answer: **Use existing database (PostgreSQL/SQLite)** for Phase 1
     - No new infrastructure required
     - Leverages existing GORM connection
     - Sufficient performance for most use cases (~10-50ms)
     - Multi-replica support via shared database

3. **Default RBAC Policy**
   - What should the default policy be if no RBAC ConfigMap exists?
   - Allow all (permissive) or deny all (secure by default)?
   - Answer: Deny all by default, require explicit policy configuration

4. **Session Storage**
   - Should we reuse existing `session` table or create new `http_session` table?
   - Can we consolidate conversational sessions and HTTP sessions?
   - Answer: Keep separate - `session` is domain model, HTTP sessions are auth concern

5. **Token Lifetime**
   - What should default token expiration be? (ArgoCD uses 24 hours)
   - Should it be configurable? Per-provider?
   - Answer: Default 24h, configurable via `--token-expiration` flag

6. **Group Claim Mapping**
   - Should we support custom claim paths (e.g., `user.groups` vs `groups`)?
   - How to handle providers that don't include groups in ID token?
   - Answer for Phase 1:
     - Support configurable claim path via `--oidc-group-claim`
     - Support UserInfo endpoint fetching if groups not in ID token
     - **No normalization** - operators must write RBAC policies matching their provider's group format

7. **CLI Token Storage**
   - Should we use OS keychain/keyring for token storage?
   - Or simple file in `~/.kagent/`?
   - Answer: Start with file storage, add keychain support in Phase 2

8. **Multi-Cluster Support**
   - How should authentication work in multi-cluster deployments?
   - Shared Dex or per-cluster?
   - Answer: Single shared Dex instance can serve multiple KAgent clusters; document this pattern

9. **Dex Deployment Guidance**
   - Should we provide reference Dex deployment manifests?
   - How opinionated should we be about Dex configuration?
   - Answer: Provide example manifests and Helm values, but don't mandate specific deployment patterns
