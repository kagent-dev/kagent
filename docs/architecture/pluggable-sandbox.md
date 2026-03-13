# Pluggable Sandbox Architecture for Kagent

**Status:** Implemented
**Created:** 2026-03-12
**Updated:** 2026-03-13
**Authors:** Eitan Yarmush, Claude (design partner)

---

## Executive Summary

Kagent supports per-session sandbox environments for agents via a pluggable `SandboxProvider` interface. The default provider uses [`kubernetes-sigs/agent-sandbox`](https://github.com/kubernetes-sigs/agent-sandbox) (pod-per-sandbox with warm pool support). Both the Python and Go ADKs integrate with the sandbox system transparently — the agent gets `exec`, `read_file`, `write_file`, `list_dir`, and `get_skill` MCP tools without knowing the underlying provider.

Kagent's role is minimal: it auto-generates a `SandboxTemplate` when an agent opts in, provisions a sandbox on first use, tells the agent where to find it, and cleans up when the session ends. All MCP tool traffic flows directly from the agent to the sandbox — the controller is not in the data path.

---

## Design Principles

1. **MCP is the contract.** Agents interact with sandboxes through MCP tools. The sandbox provider's job is to return an MCP endpoint. How it provisions the underlying sandbox is opaque.
2. **Session-scoped lifecycle.** Sandboxes are created when a session first needs one (lazy) and destroyed when the session ends. Not tied to agent pod lifecycle.
3. **Upstream-friendly default.** The default provider uses `kubernetes-sigs/agent-sandbox`, a K8s SIG project. No custom infrastructure required.
4. **Provider selection is cluster-level.** Individual agents opt into sandbox support; the cluster admin enables the feature and configures the provider.
5. **Auto-generation by default.** When an agent sets `workspace.enabled: true`, the controller auto-generates the necessary `SandboxTemplate` CR. Users can override with a custom template via `templateRef`.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                   │
│  ┌──────────┐    1. POST /api/sessions/{id}/sandbox               │
│  │  Agent    │ ────────────────────────────►  ┌───────────────┐   │
│  │ Runtime   │ ◄──── 2. 200 OK {mcp_url}───  │  Controller   │   │
│  │ (Py/Go)  │                                 │  HTTP API     │   │
│  └──────────┘                                 └───────────────┘   │
│       │                                              │            │
│       │ 3. Store mcp_url in session_state            │            │
│       │                                     Creates SandboxClaim  │
│       │ 4. Direct MCP calls                          │            │
│       ▼                                              ▼            │
│  ┌──────────────────────────────────────────────────────────┐     │
│  │ Sandbox Pod (provisioned by agent-sandbox controller)     │     │
│  │                                                           │     │
│  │  ┌─────────────────────────┐                              │     │
│  │  │ kagent-sandbox-mcp      │                              │     │
│  │  │ (StreamableHTTP :8080)  │                              │     │
│  │  │                         │                              │     │
│  │  │  - exec                 │                              │     │
│  │  │  - read_file            │                              │     │
│  │  │  - write_file           │                              │     │
│  │  │  - list_dir             │                              │     │
│  │  │  - get_skill            │                              │     │
│  │  └─────────────────────────┘                              │     │
│  └──────────────────────────────────────────────────────────┘     │
└───────────────────────────────────────────────────────────────────┘
```

**Key property:** The controller is only in the path for sandbox provisioning (steps 1-2). All MCP tool traffic flows directly from agent to sandbox.

---

## MCP Tool Contract

Every sandbox exposes an MCP server (StreamableHTTP on port 8080) with these tools. Agents discover tools via standard MCP `tools/list`.

### Tools

| Tool | Description | Parameters | Returns |
|------|-------------|------------|---------|
| `exec` | Execute a shell command via `sh -c` | `command: string`, `timeout_ms?: int`, `working_dir?: string` | `stdout: string`, `stderr: string`, `exit_code: int` |
| `read_file` | Read file contents | `path: string` | File contents as string |
| `write_file` | Write file contents (auto-creates parent dirs) | `path: string`, `content: string` | `{ok: true}` |
| `list_dir` | List directory entries | `path?: string` (default: `.`) | `entries: [{name, type: "file"\|"dir", size}]` |
| `get_skill` | Load a skill by name | `name: string` | Full `SKILL.md` content |

The `get_skill` tool is only available when skills are configured on the agent. The available skill names are embedded in the tool's description so the LLM knows what's available.

**Implementation:** `go/sandbox-mcp/pkg/tools/` — `exec.go`, `fs.go`, `skills.go`

---

## CRD: WorkspaceSpec

The Agent CRD has a `workspace` field on `DeclarativeAgentSpec`:

```go
// go/api/v1alpha2/agent_types.go

type WorkspaceSpec struct {
    // Enabled activates workspace/sandbox provisioning. When true, the
    // controller generates a SandboxTemplate and provisions a sandbox pod
    // per session with exec, filesystem, and (if configured) skill tools.
    // +kubebuilder:default=true
    Enabled bool `json:"enabled"`

    // TemplateRef optionally references a user-provided SandboxTemplate by
    // name (in the same namespace). When set, the controller uses this
    // template instead of generating one automatically.
    // +optional
    TemplateRef string `json:"templateRef,omitempty"`
}
```

### Usage

Minimal — just enable it:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: coding-agent
  namespace: kagent
spec:
  type: Declarative
  declarative:
    modelConfig: default-model-config
    systemMessage: "You are a coding agent with sandbox access."
    workspace:
      enabled: true
```

With a custom template:

```yaml
    workspace:
      enabled: true
      templateRef: my-custom-sandbox-template
```

---

## SandboxProvider Interface

Controller-internal interface in `go/core/internal/controller/sandbox/provider.go`:

```go
type SandboxProvider interface {
    // GetOrCreate provisions a new sandbox or returns the existing one for a
    // session. Implementations must be idempotent for the same session ID.
    // The call blocks until the sandbox is ready or the context is cancelled.
    GetOrCreate(ctx context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error)

    // Get returns the current sandbox endpoint for a session, or nil if none exists.
    Get(ctx context.Context, sessionID string) (*SandboxEndpoint, error)

    // Destroy tears down the sandbox for the given session ID.
    Destroy(ctx context.Context, sessionID string) error
}

type CreateSandboxOptions struct {
    SessionID    string
    AgentName    string
    Namespace    string
    WorkspaceRef WorkspaceRef
}

type WorkspaceRef struct {
    APIGroup  string
    Kind      string
    Name      string
    Namespace string
}

type SandboxEndpoint struct {
    ID       string            `json:"sandbox_id"`
    MCPUrl   string            `json:"mcp_url"`
    Protocol string            `json:"protocol"`
    Headers  map[string]string `json:"headers,omitempty"`
    Ready    bool              `json:"ready"`
}
```

### AgentSandboxProvider (default implementation)

Located in `go/core/internal/controller/sandbox/agent_sandbox_provider.go`. Uses `kubernetes-sigs/agent-sandbox` CRDs.

**How it works:**

1. `GetOrCreate` generates a deterministic claim name: `"kagent-" + sessionID` (truncated to 63 chars)
2. Attempts to fetch existing claim first (idempotent)
3. If not found, creates a `SandboxClaim` referencing the `SandboxTemplate` from `WorkspaceRef.Name`
4. Polls every 250ms (`wait.PollUntilContextCancel`) until the claim's `Ready` condition is true
5. Detects terminal failures immediately (`TemplateNotFound`, `ReconcilerError`, `SandboxExpired`, `ClaimExpired`)
6. Builds endpoint from the underlying `Sandbox` resource's `ServiceFQDN`: `http://{ServiceFQDN}:8080/mcp`

**Labels on SandboxClaim:** `kagent.dev/session-id: {sessionID}`

**Destroy:** Lists claims by session ID label, deletes all matching claims.

### StubProvider (testing)

Located in `go/core/internal/controller/sandbox/stub_provider.go`. Returns fake endpoints for unit tests.

---

## SandboxTemplate Auto-Generation

The `SandboxTemplatePlugin` (`go/core/internal/controller/translator/agent/sandbox_template_plugin.go`) is a translator plugin that auto-generates a `SandboxTemplate` CR during agent reconciliation.

**When it runs:**
- Agent is `Declarative` type
- `workspace.enabled` is `true`
- No custom `templateRef` is set

**What it generates:**

- **Name:** `{agentName}-sandbox` (truncated to 63 chars)
- **Labels:** `app: kagent`, `kagent.dev/agent: {agentName}`, `kagent.dev/component: sandbox-template`
- **Pod spec:** Single container `sandbox` running `kagent-sandbox-mcp` image on port 8080
- **Skills support:** If the agent has skills configured, adds a `skills-init` init container, an `EmptyDir` volume (`kagent-skills`), and sets `SKILLS_DIR=/skills` on the sandbox container

**Image configuration** is controlled by `DefaultSandboxMCPImageConfig`, overridable via CLI flags / helm values:
- Registry: `ghcr.io` (default)
- Repository: `kagent-dev/kagent-sandbox-mcp`
- Tag: controller version
- PullPolicy: `IfNotPresent`

---

## HTTP Endpoints

Registered in `go/core/internal/httpserver/server.go`:

### POST /api/sessions/{session_id}/sandbox

**Handler:** `SandboxHandler.HandleCreateSandbox`

1. Fetches session from DB, extracts agent reference
2. Fetches Agent CRD from Kubernetes
3. Validates `workspace.enabled` is true (400 if not)
4. Determines template name: `workspace.templateRef` or `{agentName}-sandbox`
5. Calls `Provider.GetOrCreate()` with a 5-minute timeout
6. Returns `200 OK` with `SandboxResponse`

### GET /api/sessions/{session_id}/sandbox

**Handler:** `SandboxHandler.HandleGetSandboxStatus`

Returns the current sandbox state for a session, or 404 if none exists.

### Response Type

```go
// go/api/httpapi/types.go
type SandboxResponse struct {
    SandboxID string            `json:"sandbox_id"`
    MCPUrl    string            `json:"mcp_url"`
    Protocol  string            `json:"protocol"`
    Headers   map[string]string `json:"headers,omitempty"`
    Ready     bool              `json:"ready"`
}
```

---

## Agent Runtime Integration

### Python ADK

In `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`:

**`_ensure_sandbox_toolset(session, runner, run_args)`** is called during request handling (in `_handle_request`). It:

1. Checks `session.state` for an existing `sandbox_mcp_url` — if found, reuses it
2. Otherwise, POSTs to `{KAGENT_URL}/api/sessions/{session_id}/sandbox` (30s timeout)
3. Stores `mcp_url` in session state via a system `Event` with `state_delta`
4. Appends a `KAgentMcpToolset` (StreamableHTTP connection) to `runner.agent.tools`

### Go ADK

In `go/adk/pkg/sandbox/`:

- **`SandboxProvisioner`** — HTTP client that calls `POST /api/sessions/{id}/sandbox` on the controller
- **`SandboxRegistry`** — Thread-safe map of session ID to MCP toolset, with idempotent `GetOrCreate`
- **`SandboxToolset`** — Implements `tool.Toolset`, returns sandbox tools for the current session from the registry

---

## Session Lifecycle

### Provisioning (lazy, on first message)

```
1. Agent receives first message for a session
2. ADK calls: POST /api/sessions/{session_id}/sandbox
3. Controller resolves agent → workspace config → template name
4. Controller calls provider.GetOrCreate(sessionID, templateName)
5. Provider creates SandboxClaim → agent-sandbox controller provisions pod
6. Provider polls until Ready (250ms interval, 5min timeout)
7. Controller returns 200 OK with {mcp_url: "http://{fqdn}:8080/mcp"}
8. ADK stores mcp_url in session_state, adds MCP toolset to runner
9. Agent uses sandbox tools directly for remainder of session
```

### Cleanup (on session delete)

```
1. Session is deleted (user action or timeout)
2. SessionsHandler calls provider.Destroy(sessionID) (best-effort)
3. Provider lists SandboxClaims by label kagent.dev/session-id
4. Provider deletes matching claims → agent-sandbox terminates pods
```

---

## Helm Configuration

### Values (`helm/kagent/values.yaml`)

```yaml
agentSandbox:
  enabled: false    # Feature flag — must be true to use workspaces
  image:
    registry: ghcr.io
    repository: kagent-dev/kagent-sandbox-mcp
    tag: ""         # Defaults to global tag, then Chart version
    pullPolicy: ""  # Defaults to global imagePullPolicy
```

### What `agentSandbox.enabled: true` does

1. **ConfigMap** (`controller-configmap.yaml`): Sets env vars on the controller:
   - `ENABLE_K8S_SIGS_AGENT_SANDBOX: "true"`
   - `K8S_SIGS_AGENT_SANDBOX_MCP_IMAGE_REGISTRY`
   - `K8S_SIGS_AGENT_SANDBOX_MCP_IMAGE_REPOSITORY`
   - `K8S_SIGS_AGENT_SANDBOX_MCP_IMAGE_TAG`
   - `K8S_SIGS_AGENT_SANDBOX_MCP_IMAGE_PULL_POLICY`

2. **RBAC** (`agent-sandbox-clusterrole.yaml`): Creates ClusterRole with permissions for:
   - `extensions.agents.x-k8s.io`: `sandboxclaims`, `sandboxtemplates` (full CRUD)
   - `agents.x-k8s.io`: `sandboxes` (get, list, watch)

3. **Controller startup** (`go/core/pkg/app/app.go`):
   - Checks if agent-sandbox CRDs are installed in the cluster via REST mapper
   - If found: registers `SandboxTemplatePlugin` as a translator plugin and creates `AgentSandboxProvider`
   - If not found: logs a warning, sandbox features unavailable

### Prerequisites

The `kubernetes-sigs/agent-sandbox` CRDs and controller must be installed separately. Kagent does not install them — it only creates `SandboxClaim` and `SandboxTemplate` resources that the agent-sandbox controller reconciles.

---

## Component Summary

| Component | Location | Status |
|-----------|----------|--------|
| `WorkspaceSpec` on Agent CRD | `go/api/v1alpha2/agent_types.go` | Implemented |
| `SandboxProvider` interface | `go/core/internal/controller/sandbox/provider.go` | Implemented |
| `AgentSandboxProvider` | `go/core/internal/controller/sandbox/agent_sandbox_provider.go` | Implemented |
| `SandboxTemplatePlugin` | `go/core/internal/controller/translator/agent/sandbox_template_plugin.go` | Implemented |
| HTTP sandbox endpoints | `go/core/internal/httpserver/handlers/sandbox.go` | Implemented |
| Session cleanup hook | `go/core/internal/httpserver/handlers/sessions.go` | Implemented |
| Python ADK integration | `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` | Implemented |
| Go ADK sandbox packages | `go/adk/pkg/sandbox/` | Implemented |
| `kagent-sandbox-mcp` server | `go/sandbox-mcp/` | Implemented |
| Helm chart support | `helm/kagent/` | Implemented |
| MoatProvider (internal) | — | Not yet implemented |

---

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| Auto-generate SandboxTemplate | Simplifies user experience — just set `workspace.enabled: true` |
| Blocking `GetOrCreate` (not 202) | Simpler agent logic — no polling needed, controller handles the wait |
| MCP URL in session_state | Agent runtime stores endpoint, connects directly |
| Provider selection is cluster-level | Admin configures once via helm, agents just opt in |
| `kagent-sandbox-mcp` in this repo | Small image, tightly coupled to the MCP tool contract |
| Lazy provisioning on first message | No wasted resources for sessions that never use sandbox |
| Best-effort cleanup on session delete | Provider handles idempotent destroy |
| Deterministic claim name from session ID | Enables idempotent GetOrCreate without external state |
