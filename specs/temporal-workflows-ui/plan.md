# Implementation Plan

## Checklist

- [ ] Step 1: Scaffold Go plugin binary with config and Temporal client
- [ ] Step 2: Implement Temporal client wrapper (list, describe, cancel, signal)
- [ ] Step 3: Implement 4 MCP tools
- [ ] Step 4: Implement REST API handlers
- [ ] Step 5: Implement SSE hub with Temporal polling
- [ ] Step 6: Build embedded SPA (workflow list + detail + actions)
- [ ] Step 7: Wire HTTP server and main.go entry point
- [ ] Step 8: Create Helm chart and RemoteMCPServer CRD
- [ ] Step 9: Update stock Temporal UI CRD (AGENTS section) and remove stub page
- [ ] Step 10: Tests (unit + integration)
- [ ] Step 11: E2E test in Kind cluster

---

## Step 1: Scaffold Go plugin binary with config

**Objective:** Create the Go module, directory structure, and config loading.

**Implementation:**
- Create `go/plugins/temporal-mcp/` with `go.mod` (module `github.com/kagent-dev/kagent/go/plugins/temporal-mcp`)
- `internal/config/config.go` — struct with CLI flags + `TEMPORAL_*` env var fallback
- Fields: `Addr`, `Transport`, `TemporalHostPort`, `TemporalNamespace`, `PollInterval`, `LogLevel`
- Defaults: `:8080`, `http`, `temporal-server:7233`, `kagent`, `5s`, `info`

**Tests:** `internal/config/config_test.go` — verify defaults, env var override, flag override.

**Demo:** `go build ./go/plugins/temporal-mcp/` compiles, `--help` shows flags.

---

## Step 2: Implement Temporal client wrapper

**Objective:** Wrap Temporal Go SDK for workflow listing, detail, cancel, and signal.

**Implementation:**
- `internal/temporal/client.go` — `NewClient(hostPort, namespace)`, `Close()`
- `ListWorkflows(ctx, filter)` — uses `client.ListWorkflow()` with visibility query strings
  - Build query: `WorkflowType = 'AgentExecutionWorkflow'` + optional status/agent filters
  - Parse workflow ID (`agent-{name}-{session}`) to extract AgentName, SessionID
  - Return `[]*WorkflowSummary`
- `GetWorkflow(ctx, workflowID)` — uses `client.DescribeWorkflowExecution()` + `GetWorkflowHistory()`
  - Extract activity info from history events (ActivityTaskScheduled, ActivityTaskCompleted, ActivityTaskFailed)
  - Return `*WorkflowDetail` with `[]ActivityInfo`
- `CancelWorkflow(ctx, workflowID)` — uses `client.CancelWorkflow()`
- `SignalWorkflow(ctx, workflowID, signalName, data)` — uses `client.SignalWorkflow()`
- `internal/temporal/types.go` — `WorkflowFilter`, `WorkflowSummary`, `WorkflowDetail`, `ActivityInfo`
- `internal/temporal/parse.go` — `ParseWorkflowID(id) (agentName, sessionID)`

**Tests:** `internal/temporal/client_test.go` — mock Temporal client interface, test query building, workflow ID parsing, error handling.

**Demo:** Unit tests pass with mocked Temporal client.

---

## Step 3: Implement 4 MCP tools

**Objective:** Register MCP tools that AI agents can invoke for workflow administration.

**Implementation:**
- `internal/mcp/tools.go` — `NewServer(tc *temporal.Client) *mcpsdk.Server`
- 4 tools: `list_workflows`, `get_workflow`, `cancel_workflow`, `signal_workflow`
- Input types: `listWorkflowsInput`, `getWorkflowInput`, `cancelWorkflowInput`, `signalWorkflowInput`
- Handler functions follow kanban-mcp pattern: return `textResult(v)` or `errorResult(msg)`
- `list_workflows` input: `status?`, `agent_name?`, `page_size?` (default 50)
- `get_workflow` input: `workflow_id`
- `cancel_workflow` input: `workflow_id`
- `signal_workflow` input: `workflow_id`, `signal_name`, `data?` (JSON string)

**Tests:** `internal/mcp/tools_test.go` — mock temporal client, verify each tool returns correct MCP result shape.

**Demo:** MCP tools callable via stdio transport with mock Temporal.

---

## Step 4: Implement REST API handlers

**Objective:** REST endpoints for the embedded UI to consume.

**Implementation:**
- `internal/api/handlers.go`
- `WorkflowsHandler(tc)` — `GET /api/workflows?status=running&agent=k8s-agent`
  - Parse query params, call `tc.ListWorkflows()`, respond JSON
- `WorkflowHandler(tc)` — routes by path suffix:
  - `GET /api/workflows/{id}` — call `tc.GetWorkflow()`, respond JSON
  - `POST /api/workflows/{id}/cancel` — call `tc.CancelWorkflow()`, respond `{canceled: true}`
  - `POST /api/workflows/{id}/signal` — parse body `{signal_name, data}`, call `tc.SignalWorkflow()`
- Standard JSON response envelope: `{"data": ..., "error": "..."}`

**Tests:** `internal/api/handlers_test.go` — httptest with mock temporal client, verify status codes and response shapes.

**Demo:** `curl localhost:8080/api/workflows` returns JSON (against dev Temporal server).

---

## Step 5: Implement SSE hub with Temporal polling

**Objective:** Live workflow status updates pushed to connected UI clients.

**Implementation:**
- `internal/sse/hub.go` — `NewHub(tc, interval)`, `Start(ctx)`, `ServeSSE(w, r)`
- Background goroutine polls `tc.ListWorkflows()` every `interval`
- Compare with previous snapshot to detect changes (new workflows, status transitions)
- On connect: send `event: snapshot\ndata: {workflows: [...]}\n\n`
- On change: send `data: {type: "workflow_update", data: {...}}\n\n`
- Client map with mutex, cleanup on disconnect
- Badge: count of running workflows, broadcast via SSE for plugin bridge

**Tests:** `internal/sse/hub_test.go` — mock temporal client, verify snapshot on connect, update broadcasts.

**Demo:** `curl -N localhost:8080/events` streams workflow updates.

---

## Step 6: Build embedded SPA

**Objective:** Single HTML file with workflow list, detail view, actions, live updates.

**Implementation:**
- `internal/ui/index.html` — single file with `<style>` + `<script>`
- `internal/ui/embed.go` — `//go:embed index.html`, `Handler()` returns `http.Handler`

**UI layout:**
- **Header:** "Temporal Workflows" title, running workflow count badge
- **Filter tabs:** All | Running | Completed | Failed (clickable, URL hash-based)
- **Workflow table:** Agent, Workflow ID (truncated), Status badge, Start Time, Duration
- **Click row:** expand detail panel with activity timeline (name, status, duration, attempt, error)
- **Actions:** Cancel button (running only), Signal button with modal (signal name + JSON data)
- **Empty state:** Icon + "No workflows found" when filtered list is empty
- **Error banner:** Shown when Temporal connection fails

**Theme:** CSS variables, dark/light via `kagent.onContext()` bridge
**Badge:** `kagent.setBadge(runningCount)` on each SSE update
**SSE:** Connect to `/events`, handle `snapshot` + `data` events, auto-reconnect

**Tests:** `internal/ui/embed_test.go` — verify embed loads, content-type correct.

**Demo:** Open `localhost:8080` in browser, see workflow list with live updates.

---

## Step 7: Wire HTTP server and main.go

**Objective:** Complete the binary entry point wiring all components.

**Implementation:**
- `server.go` — `NewHTTPServer(cfg, tc, hub)` wires mux with all routes
- `main.go`:
  1. Load config
  2. Create Temporal client (`temporal.NewClient()`)
  3. Create SSE hub, start background poller
  4. If stdio transport: run MCP server on stdio
  5. If http transport: create HTTP server, listen, graceful shutdown

**Tests:** `server_test.go` — verify all routes registered, 200 on `/`, `/api/workflows`.

**Demo:** `go run ./go/plugins/temporal-mcp/` starts, serves UI, responds to API calls.

---

## Step 8: Create Helm chart and RemoteMCPServer CRD

**Objective:** Deployable to K8s with plugin auto-registration.

**Implementation:**
- `helm/tools/temporal-mcp/Chart.yaml` — name, version, description
- `helm/tools/temporal-mcp/values.yaml` — image, replicas, resources, config env vars
- `helm/tools/temporal-mcp/templates/`:
  - `_helpers.tpl` — fullname, labels, serverUrl helpers
  - `deployment.yaml` — single container, env from configmap, no volumes (stateless)
  - `service.yaml` — ClusterIP :8080
  - `configmap.yaml` — `TEMPORAL_*` env vars from values
  - `remotemcpserver.yaml` — registers as plugin with `ui.enabled: true`, pathPrefix `temporal-workflows`, section `PLUGINS`

**Tests:** `helm lint helm/tools/temporal-mcp`, `helm template test helm/tools/temporal-mcp`.

**Demo:** `helm install temporal-mcp helm/tools/temporal-mcp` deploys to cluster, plugin appears in sidebar.

---

## Step 9: Update stock Temporal UI CRD and remove stub page

**Objective:** AGENTS/Workflows points to stock Temporal UI; remove hardcoded stub.

**Implementation:**
- Edit `helm/kagent/templates/temporal-ui-remotemcpserver.yaml`:
  - Change `section: "PLUGINS"` → `section: "AGENTS"`
  - Change `displayName: "Temporal Workflows"` → `displayName: "Workflows"`
  - Keep `pathPrefix: "temporal"`, `icon: "git-branch"`
- Remove hardcoded "Workflows" entry from `ui/src/components/sidebars/AppSidebarNav.tsx` NAV_SECTIONS (AGENTS section)
- Delete stub page `ui/src/app/workflows/page.tsx`

**Integration notes:** When Temporal is disabled in Helm, neither "Workflows" nor "Temporal Workflows" appears in sidebar (no RemoteMCPServer CRDs deployed). When enabled, both appear: AGENTS/Workflows (stock UI) and PLUGINS/Temporal Workflows (custom plugin).

**Tests:** Verify sidebar renders correctly with/without Temporal enabled.

**Demo:** Sidebar shows "Workflows" under AGENTS linking to stock Temporal UI iframe.

---

## Step 10: Tests (unit + integration)

**Objective:** Comprehensive test coverage.

**Implementation:**
- Unit tests (already created per-step): config, temporal client, MCP tools, REST handlers, SSE hub, embed
- Integration test: `internal/temporal/integration_test.go`
  - Requires running Temporal dev server
  - Build-tag guarded: `//go:build integration`
  - Start a test workflow, verify ListWorkflows returns it, verify GetWorkflow shows activities, cancel it, verify status change

**Tests:** `go test ./go/plugins/temporal-mcp/...`

**Demo:** All unit tests green. Integration tests green against local Temporal dev server.

---

## Step 11: E2E test in Kind cluster

**Objective:** Verify full plugin lifecycle in real K8s environment.

**Implementation:**
- Add test in `go/core/test/e2e/` or as a separate test script
- Prerequisites: Kind cluster with kagent + Temporal + temporal-mcp Helm charts
- Verify:
  1. `GET /api/plugins` includes temporal-workflows plugin
  2. `GET /_p/temporal-workflows/` returns HTML
  3. `GET /_p/temporal-workflows/api/workflows` returns JSON (possibly empty list)
  4. `GET /_p/temporal/` returns stock Temporal UI HTML
  5. Sidebar renders both entries correctly

**Demo:** E2E test passes in CI with Kind cluster.
