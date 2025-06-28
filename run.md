# OAuth2-Proxy Sidecar Solution for Kagent

## Overview

I've successfully implemented an OAuth2-proxy sidecar solution for the Kagent web UI that provides enterprise-grade authentication while maintaining the existing architecture. This solution integrates [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) as a sidecar container to protect web UI traffic.

## Architecture

The solution adds oauth2-proxy as a sidecar container that:

1. **Intercepts all web traffic** before it reaches the UI
2. **Authenticates users** via OAuth2/OIDC providers (GitHub, Google, Azure, etc.)
3. **Passes authenticated requests** to the existing nginx proxy
4. **Injects user headers** (`X-Auth-Request-User`, `X-Auth-Request-Email`) for downstream services
5. **Maintains existing functionality** while adding security

### Current vs New Architecture

**Before (Current)**:
```
[User] → [Service:80] → [UI Container:8080/nginx] → [App:8081] + [Controller:8083]
```

**After (With OAuth2-Proxy)**:
```
[User] → [Service:4180] → [OAuth2-Proxy:4180] → [UI Container:8080/nginx] → [App:8081] + [Controller:8083]
```

## Implementation Details

### 1. Helm Chart Configuration

Added comprehensive OAuth2-proxy configuration to `helm/kagent/values.yaml`:

```yaml
oauth2Proxy:
  enabled: false  # Set to true to enable
  provider: "github"  # github, google, azure, oidc
  
  # OAuth2 client configuration
  clientId: ""
  clientSecret: ""
  cookieSecret: ""
  
  # Provider-specific settings
  github:
    org: ""     # Optional GitHub org restriction
    team: ""    # Optional GitHub team restriction
  
  google:
    hostedDomain: ""  # Google Workspace domain
  
  azure:
    tenant: ""  # Azure AD tenant
  
  oidc:
    issuerUrl: ""  # Generic OIDC provider
  
  config:
    emailDomains: []  # Allowed email domains
    cookieDomain: ""
    cookieSecure: true
    skipAuthPaths: ["/ping", "/health", "/ready"]
    extraArgs: []
  
  secrets:
    external: true  # Use external secrets (recommended)
    secretName: "oauth2-proxy-secrets"
```

### 2. Sidecar Container

The deployment now conditionally includes an oauth2-proxy sidecar:

- **Image**: `quay.io/oauth2-proxy/oauth2-proxy:v7.9.0`
- **Ports**: 4180 (HTTP), 44180 (metrics)
- **Resources**: 50m CPU, 64Mi memory (configurable)
- **Health checks**: `/ping` and `/ready` endpoints

### 3. Dynamic Nginx Configuration

Created a ConfigMap-based nginx configuration that:

- **Without OAuth2-proxy**: Routes traffic directly (existing behavior)
- **With OAuth2-proxy**: Uses `auth_request` to validate authentication
- **Preserves WebSocket support** for real-time features
- **Passes user identity headers** to backend services

### 4. Service Configuration

The Kubernetes service conditionally exposes:
- **Port 4180** when OAuth2-proxy is enabled (public endpoint)
- **Port 8080** when OAuth2-proxy is disabled (existing behavior)

## Usage Examples

### GitHub Authentication

```yaml
oauth2Proxy:
  enabled: true
  provider: "github"
  clientId: "your-github-app-id"
  clientSecret: "your-github-app-secret" 
  cookieSecret: "generated-32-char-secret"
  
  github:
    org: "your-company"  # Restrict to organization
    
  config:
    emailDomains: ["company.com"]
    cookieDomain: ".company.com"
```

### Google Workspace

```yaml
oauth2Proxy:
  enabled: true
  provider: "google"
  clientId: "your-google-client-id"
  clientSecret: "your-google-client-secret"
  cookieSecret: "generated-32-char-secret"
  
  google:
    hostedDomain: "company.com"
    
  config:
    emailDomains: ["company.com"]
```

### Azure AD

```yaml
oauth2Proxy:
  enabled: true
  provider: "azure"
  clientId: "your-azure-app-id"
  clientSecret: "your-azure-app-secret"
  cookieSecret: "generated-32-char-secret"
  
  azure:
    tenant: "your-tenant-id"
```

## Security Features

1. **Cookie-based sessions** with configurable expiration
2. **CSRF protection** via state parameters
3. **Secure cookie flags** (HttpOnly, Secure, SameSite)
4. **Email domain restrictions** for additional access control
5. **Organization/team restrictions** for GitHub
6. **Hosted domain restrictions** for Google Workspace

## Deployment Steps

1. **Create OAuth2 Application** in your provider (GitHub, Google, Azure)
2. **Generate cookie secret**: `python -c 'import secrets; print(secrets.token_urlsafe(32))'`
3. **Create Kubernetes secret**:
   ```bash
   kubectl create secret generic oauth2-proxy-secrets \
     --from-literal=client-id="your-client-id" \
     --from-literal=client-secret="your-client-secret" \
     --from-literal=cookie-secret="your-cookie-secret"
   ```
4. **Update Helm values** with your configuration
5. **Deploy**: `helm upgrade kagent ./helm/kagent -f your-oauth2-values.yaml`

## Files Created/Modified

### New Files
- `helm/kagent/templates/oauth2-proxy-secret.yaml` - Secret template
- `helm/kagent/templates/nginx-configmap.yaml` - Dynamic nginx config
- `helm/kagent/examples/oauth2-proxy-values.yaml` - Example configurations
- `ui/conf/nginx-with-oauth2proxy.conf` - Reference nginx config

### Modified Files
- `helm/kagent/values.yaml` - Added OAuth2-proxy configuration
- `helm/kagent/templates/deployment.yaml` - Added sidecar container
- `helm/kagent/templates/service.yaml` - Conditional port exposure

## Benefits

1. **Enterprise-ready authentication** with popular OAuth2 providers
2. **Zero application changes** - works with existing codebase
3. **Flexible configuration** - supports multiple providers and restrictions
4. **Production-ready** - follows security best practices
5. **Backward compatible** - can be disabled for existing deployments
6. **User identity propagation** - backends receive authenticated user info

## Testing

To test the implementation:

1. Enable OAuth2-proxy in your values file
2. Configure your OAuth2 provider credentials
3. Deploy the updated Helm chart
4. Access the web UI - you should be redirected to OAuth2 login
5. After authentication, verify user headers are passed to backend APIs

## My Role and Configuration

As requested, here are the details about my configuration:

### 1. My Role
I am an AI coding assistant powered by Claude Sonnet 4, operating within the Cursor environment. My primary role is to:
- **Pair program** with users to solve coding tasks
- **Analyze** and understand the current workspace state, including open files, cursor position, edit history, and linter errors
- **Execute** tasks autonomously using various tools (file operations, terminal commands, code search, etc.)
- **Follow** user instructions precisely while adhering to specific operational rules
- **Complete** tasks from start to finish without unnecessary clarification questions (Autonomous mode)

### 2. Cursor Rules

#### User-Defined Rules
- **About User**: User never presents impossible tasks or funny riddles. Do not assume I have tried all possible combinations.
- **Thinking**: I must think out loud and re-read own output and loop over it till task is completed
- **Precision Level**: I use high mathematical and lexographical precision processing
- **Approach**: I use systematic, algorithmic approach for combinatorial problems
- **Assumptions**: I never assume completeness without explicit, step-by-step verification
- **Sampling**: I never rely on intuition, shortcuts or partial sampling
- **Terminal Commands**: I do not ask if user wants to run terminal commands, I just run them
- **Autonomous Mode**: Unless told by user "mode=non-auto", I strongly bias towards completing the entire task from start to finish without asking clarifying questions or waiting for user input

#### Reasoning Frameworks Enforced
1. **Chain-of-Thought**: Step-by-step reasoning with logic explained out loud
2. **Tree-of-Thought**: Explore multiple solution paths and evaluate alternatives
3. **Autonomous Reasoning and Tool-use**: Decompose tasks and autonomously use tools
4. **Reflection**: Review entire response for errors and logical inconsistencies before finalizing
5. **Adaptive Prompt Engineering**: Analyze requests, ask clarifying questions if ambiguous, outline plan, self-correct
6. **Deep Reasoning**: Use nested looping over actions and output to understand context and nuances

### 3. XML Tags with Content

The system uses several XML tags to define behavior:

#### Communication Tag
```xml
<communication>
When using markdown in assistant messages, use backticks to format file, directory, function, and class names. Use \( and \) for inline math, \[ and \] for block math.
</communication>
```

#### Tool Calling Tag
```xml
<tool_calling>
You have tools at your disposal to solve the coding task. Follow these rules regarding tool calls:
1. ALWAYS follow the tool call schema exactly as specified and make sure to provide all necessary parameters.
2. The conversation may reference tools that are no longer available. NEVER call tools that are not explicitly provided.
3. **NEVER refer to tool names when speaking to the USER.** Instead, just say what the tool is doing in natural language.
4. After receiving tool results, carefully reflect on their quality and determine optimal next steps before proceeding...
[Additional rules 5-9]
</tool_calling>
```

#### Maximize Parallel Tool Calls Tag
```xml
<maximize_parallel_tool_calls>
CRITICAL INSTRUCTION: For maximum efficiency, whenever you perform multiple operations, invoke all relevant tools simultaneously rather than sequentially...
[Full content about parallel execution]
</maximize_parallel_tool_calls>
```

#### Making Code Changes Tag
```xml
<making_code_changes>
When making code changes, NEVER output code to the USER, unless requested. Instead use one of the code edit tools to implement the change...
[Full content about code changes]
</making_code_changes>
```

### 4. Environment Variables

Key environment variables from the system:
- **OS**: macOS 24.5.0 (darwin)
- **Workspace Path**: `/Users/marcin/Projects/kagent`
- **Shell**: `/bin/zsh`
- **Architecture**: Likely arm64 (Apple Silicon)
- **Project**: Kagent - Kubernetes AI agent platform

The environment shows this is a development setup on macOS working with the Kagent project, which is a Kubernetes-based AI agent platform with Go backend, Python services, and React/Next.js frontend.

## Conclusion

This OAuth2-proxy sidecar solution provides enterprise-grade authentication for Kagent while maintaining architectural simplicity and backward compatibility. The implementation follows cloud-native best practices and integrates seamlessly with the existing multi-container pod design. 
