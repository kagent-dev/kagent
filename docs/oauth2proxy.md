# OAuth2-Proxy Authentication for Kagent

## Overview

Kagent supports OAuth2-proxy as a sidecar container to provide enterprise-grade authentication for the web UI. This integration allows you to secure your Kagent deployment with popular OAuth2/OIDC providers like GitHub, Google, Azure AD, and others.

## Architecture

The OAuth2-proxy sidecar intercepts all web traffic before it reaches the UI, authenticating users via your chosen OAuth2 provider and then passing authenticated requests to the existing nginx proxy.

```
┌─────────┐    ┌──────────────┐    ┌─────────────┐    ┌─────────────┐
│  User   │───▶│ OAuth2-Proxy │───▶│ Nginx Proxy │───▶│ Backend API │
│         │    │   (Port 4180)│    │ (Port 8080) │    │ (Port 8081) │
└─────────┘    └──────────────┘    └─────────────┘    └─────────────┘
                      │
                      ▼
               ┌──────────────┐
               │ OAuth2       │
               │ Provider     │
               │ (GitHub/etc) │
               └──────────────┘
```

## Step-by-Step: What Happens When

### Scenario 1: OAuth2-Proxy Disabled (Default Behavior)

**Default Configuration**: `oauth2Proxy.enabled: false` ✅

When OAuth2-proxy is disabled (the default), here's what happens step by step:

1. **User accesses Kagent** → `https://kagent.company.com`
2. **Kubernetes Service** routes traffic to **UI container port 8080**
3. **Nginx** (in UI container) receives the request directly
4. **No authentication check** - request proceeds immediately
5. **Nginx routes based on path**:
   - `/` → Next.js frontend (port 8001)
   - `/api/` → Backend API (port 8083)
   - `/api/ws/` → WebSocket backend (port 8081)
6. **User sees Kagent UI** immediately without any login

```
┌─────────┐    ┌─────────────────┐    ┌─────────────┐
│  User   │───▶│ Service:80      │───▶│ Backend API │
│         │    │ ↓               │    │ (Port 8081) │
│         │    │ Nginx:8080      │    │             │
└─────────┘    │ (No Auth)       │    └─────────────┘
               └─────────────────┘
```

**Result**: Direct access to Kagent UI with no authentication required.

### Scenario 2: OAuth2-Proxy Enabled with GitHub

**Configuration**: `oauth2Proxy.enabled: true` with GitHub provider

When OAuth2-proxy is enabled, here's the complete authentication flow:

#### Initial Access (Unauthenticated User)

1. **User accesses Kagent** → `https://kagent.company.com`
2. **Kubernetes Service** routes traffic to **OAuth2-Proxy container port 4180**
3. **OAuth2-Proxy checks for authentication cookie** → ❌ Not found
4. **OAuth2-Proxy redirects** → `https://github.com/login/oauth/authorize?client_id=...`
5. **User sees GitHub login page**

#### GitHub Authentication

6. **User logs into GitHub** (username/password or SSO)
7. **GitHub asks for app permissions** → User clicks "Authorize"
8. **GitHub redirects back** → `https://kagent.company.com/oauth2/callback?code=...`
9. **OAuth2-Proxy receives callback**:
   - Exchanges code for GitHub access token
   - Fetches user info from GitHub API
   - Validates user against configured restrictions (org/team/email)
   - Creates encrypted session cookie

#### Successful Authentication

10. **OAuth2-Proxy sets secure cookie** and redirects to original URL
11. **User accesses Kagent again** → `https://kagent.company.com`
12. **OAuth2-Proxy validates cookie** → ✅ Valid session found
13. **OAuth2-Proxy proxies request** to Nginx (port 8080) with auth headers:
    - `X-Auth-Request-User: github-username`
    - `X-Auth-Request-Email: user@example.com`
    - `X-Auth-Request-Preferred-Username: github-username`
14. **Nginx receives authenticated request** and routes normally
15. **User sees Kagent UI** with full access

```
┌─────────┐    ┌──────────────┐    ┌─────────────────┐    ┌─────────────┐
│  User   │───▶│ Service:4180 │───▶│ OAuth2-Proxy    │───▶│ Backend API │
│         │    │              │    │ ↓               │    │             │
│         │    │              │    │ Nginx:8080      │    │             │
└─────────┘    └──────────────┘    │ (Auth Headers)  │    └─────────────┘
                      │            └─────────────────┘
                      ▼
               ┌──────────────┐
               │ GitHub OAuth │
               │ (First time) │
               └──────────────┘
```

#### Example GitHub Configuration

```yaml
oauth2Proxy:
  enabled: true
  provider: "github"
  
  # GitHub OAuth App credentials
  clientId: "Iv1.a1b2c3d4e5f6g7h8"
  clientSecret: "1234567890abcdef1234567890abcdef12345678"
  cookieSecret: "very-secure-32-character-secret-key"
  
  # Restrict access to your organization
  github:
    org: "my-company"
    team: "platform-team"  # Optional: further restrict to team
  
  config:
    # Only allow company email addresses
    emailDomains: ["company.com"]
    cookieDomain: ".company.com"
    cookieSecure: true
```

**Result**: Only authenticated GitHub users from `my-company` organization with `@company.com` email addresses can access Kagent.

#### Ongoing Sessions

- **Subsequent visits**: OAuth2-Proxy validates the cookie, no GitHub redirect needed
- **Session expiry**: After 7 days (default), user must re-authenticate
- **Logout**: User can visit `/oauth2/sign_out` to clear session

## Supported Providers

- **GitHub** - Organization and team restrictions
- **Google** - Google Workspace domain restrictions  
- **Azure AD** - Tenant-based authentication
- **Generic OIDC** - Any OpenID Connect provider
- **And many more** - See [oauth2-proxy providers](https://oauth2-proxy.github.io/oauth2-proxy/docs/configuration/providers/)

## Quick Start

### 1. Enable OAuth2-Proxy

In your Helm values file, enable OAuth2-proxy:

```yaml
oauth2Proxy:
  enabled: true
  provider: "github"  # or google, azure, oidc
```

### 2. Configure Your Provider

#### GitHub Example

```yaml
oauth2Proxy:
  enabled: true
  provider: "github"
  
  # OAuth2 credentials (use external secrets in production)
  clientId: "your-github-app-id"
  clientSecret: "your-github-app-secret"
  cookieSecret: "your-32-char-random-secret"
  
  # GitHub-specific settings
  github:
    org: "your-organization"     # Optional: restrict to org
    team: "your-team"           # Optional: restrict to team
  
  config:
    emailDomains: ["company.com"]  # Optional: restrict email domains
    cookieDomain: ".company.com"   # Set to your domain
```

#### Google Workspace Example

```yaml
oauth2Proxy:
  enabled: true
  provider: "google"
  
  clientId: "your-google-client-id"
  clientSecret: "your-google-client-secret"
  cookieSecret: "your-32-char-random-secret"
  
  google:
    hostedDomain: "company.com"  # Restrict to workspace domain
  
  config:
    emailDomains: ["company.com"]
```

#### Azure AD Example

```yaml
oauth2Proxy:
  enabled: true
  provider: "azure"
  
  clientId: "your-azure-app-id"
  clientSecret: "your-azure-app-secret"
  cookieSecret: "your-32-char-random-secret"
  
  azure:
    tenant: "your-tenant-id"
```

### 3. Create Secrets

Generate a cookie secret:
```bash
python -c 'import secrets; print(secrets.token_urlsafe(32))'
```

Create the Kubernetes secret:
```bash
kubectl create secret generic oauth2-proxy-secrets \
  --from-literal=client-id="your-client-id" \
  --from-literal=client-secret="your-client-secret" \
  --from-literal=cookie-secret="your-cookie-secret"
```

### 4. Deploy

```bash
helm upgrade kagent ./helm/kagent -f your-oauth2-values.yaml
```

## Configuration Reference

### Basic Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `oauth2Proxy.enabled` | Enable OAuth2-proxy sidecar | `false` |
| `oauth2Proxy.provider` | OAuth2 provider (github, google, azure, oidc) | `"github"` |
| `oauth2Proxy.clientId` | OAuth2 client ID | `""` |
| `oauth2Proxy.clientSecret` | OAuth2 client secret | `""` |
| `oauth2Proxy.cookieSecret` | Cookie encryption secret (32 chars) | `""` |

### Provider-Specific Settings

#### GitHub
| Parameter | Description |
|-----------|-------------|
| `oauth2Proxy.github.org` | Restrict to GitHub organization |
| `oauth2Proxy.github.team` | Restrict to GitHub team |

#### Google
| Parameter | Description |
|-----------|-------------|
| `oauth2Proxy.google.hostedDomain` | Restrict to Google Workspace domain |

#### Azure AD
| Parameter | Description |
|-----------|-------------|
| `oauth2Proxy.azure.tenant` | Azure AD tenant ID |

#### OIDC
| Parameter | Description |
|-----------|-------------|
| `oauth2Proxy.oidc.issuerUrl` | OIDC provider issuer URL |

### Advanced Configuration

```yaml
oauth2Proxy:
  config:
    # Email domain restrictions
    emailDomains: ["company.com", "partner.org"]
    
    # Cookie settings
    cookieDomain: ".company.com"
    cookieSecure: true
    cookieHttpOnly: true
    cookieSameSite: "lax"
    cookieExpire: "168h"  # 7 days
    
    # Session configuration
    sessionStoreType: "cookie"
    
    # Logging
    logLevel: "info"
    
    # Skip authentication for specific paths
    skipAuthPaths:
      - "/ping"
      - "/health"
      - "/ready"
      - "/metrics"
    
    # Additional oauth2-proxy arguments
    extraArgs:
      - "--skip-provider-button"
      - "--pass-basic-auth=false"
      - "--pass-access-token"
      - "--set-xauthrequest"
```

## Security Features

### Access Control
- **Email domain restrictions** - Limit access to specific domains
- **Organization/team restrictions** - GitHub org/team membership
- **Hosted domain restrictions** - Google Workspace domains
- **Tenant restrictions** - Azure AD tenants

### Session Security
- **Secure cookies** - HttpOnly, Secure, SameSite flags
- **Session encryption** - AES-256 encrypted session cookies
- **CSRF protection** - State parameter validation
- **Configurable expiration** - Session timeout controls

### User Context Propagation
OAuth2-proxy passes authenticated user information to backend services via headers:
- `X-Auth-Request-User` - Username
- `X-Auth-Request-Email` - Email address
- `X-Auth-Request-Preferred-Username` - Preferred username

## Provider Setup Guides

### GitHub OAuth App Setup

1. Go to GitHub Settings → Developer settings → OAuth Apps
2. Click "New OAuth App"
3. Fill in details:
   - **Application name**: Kagent
   - **Homepage URL**: `https://kagent.company.com`
   - **Authorization callback URL**: `https://kagent.company.com/oauth2/callback`
4. Note the Client ID and generate a Client Secret

### Google OAuth Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable Google+ API
4. Go to Credentials → Create Credentials → OAuth 2.0 Client ID
5. Configure:
   - **Application type**: Web application
   - **Authorized redirect URIs**: `https://kagent.company.com/oauth2/callback`

### Azure AD App Registration

1. Go to [Azure Portal](https://portal.azure.com/)
2. Navigate to Azure Active Directory → App registrations
3. Click "New registration"
4. Configure:
   - **Name**: Kagent
   - **Redirect URI**: `https://kagent.company.com/oauth2/callback`
5. Note the Application (client) ID and create a client secret

## Troubleshooting

### Common Issues

#### Authentication Loop
**Symptoms**: Redirected to login repeatedly
**Solution**: Check cookie domain settings and ensure HTTPS is properly configured

#### Access Denied
**Symptoms**: "Access denied" after successful OAuth login
**Solution**: Verify email domain restrictions and organization/team settings

#### WebSocket Issues
**Symptoms**: Real-time features not working
**Solution**: Ensure WebSocket paths are properly configured in nginx

### Debug Mode

Enable debug logging:
```yaml
oauth2Proxy:
  config:
    logLevel: "debug"
    extraArgs:
      - "--cookie-debug"
```

### Health Checks

OAuth2-proxy exposes health endpoints:
- `/ping` - Basic health check
- `/ready` - Readiness check
- `/oauth2/userinfo` - User information (authenticated)

## Production Considerations

### External Secrets Management

Use external secrets instead of storing credentials in values:

```yaml
oauth2Proxy:
  secrets:
    external: true
    secretName: "oauth2-proxy-secrets"
```

### High Availability

For production deployments:
- Use Redis for session storage (requires additional configuration)
- Configure appropriate resource limits
- Set up monitoring and alerting

### SSL/TLS

Ensure proper SSL/TLS configuration:
- Use valid SSL certificates
- Set `cookieSecure: true`
- Configure proper cookie domains

## Migration from Existing Auth

If you have existing authentication:

1. **Plan the migration** - Coordinate with users
2. **Test thoroughly** - Use staging environment
3. **Gradual rollout** - Consider blue/green deployment
4. **Backup plan** - Be ready to disable if issues arise

## Resources

- [OAuth2-Proxy Documentation](https://oauth2-proxy.github.io/oauth2-proxy/)
- [Provider Configuration](https://oauth2-proxy.github.io/oauth2-proxy/docs/configuration/providers/)
- [Configuration Options](https://oauth2-proxy.github.io/oauth2-proxy/docs/configuration/overview/)
- [Example Configurations](../helm/kagent/examples/oauth2-proxy-values.yaml) 
