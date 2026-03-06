# PROMPT: Temporal Workflows MCP Plugin

## Objective

Build `temporal-mcp` — a stateless Go binary plugin for KAgent that provides Temporal workflow administration via MCP tools and an embedded web UI. Same architecture as `go/plugins/kanban-mcp/`.

## Key Requirements

- Go binary at `go/plugins/temporal-mcp/` with MCP tools, REST API, embedded SPA, SSE
- 4 MCP tools: `list_workflows`, `get_workflow`, `cancel_workflow`, `signal_workflow`
- REST API: `GET /api/workflows`, `GET /api/workflows/:id`, `POST .../cancel`, `POST .../signal`
- SSE at `/events` — polls Temporal every 5s, broadcasts workflow status changes
- Embedded single-file SPA (`internal/ui/index.html`) — vanilla JS, no build step
- Stateless — connects directly to Temporal Server gRPC, no local DB
- Helm chart at `helm/tools/temporal-mcp/` with RemoteMCPServer CRD (`section: "PLUGINS"`)
- Update `helm/kagent/templates/temporal-ui-remotemcpserver.yaml`: section → "AGENTS", displayName → "Workflows"
- Remove hardcoded "Workflows" from `ui/src/components/sidebars/AppSidebarNav.tsx` NAV_SECTIONS
- Delete stub page `ui/src/app/workflows/page.tsx`
- Config via env vars: `TEMPORAL_HOST_PORT`, `TEMPORAL_NAMESPACE` (default "kagent")

## Acceptance Criteria (Given-When-Then)

- Given temporal-mcp deployed, When user opens PLUGINS/Temporal Workflows, Then embedded SPA shows workflow list
- Given running workflows exist, When SPA is open, Then workflows appear with live SSE updates
- Given user clicks Running filter, Then only running workflows shown
- Given user clicks a workflow row, Then activity detail panel expands
- Given user clicks Cancel on running workflow, Then workflow is canceled
- Given AI agent calls `list_workflows(status=running)`, Then JSON list of running workflows returned
- Given stock Temporal UI CRD has `section: "AGENTS"`, Then "Workflows" appears under AGENTS in sidebar
- Given Temporal disabled in Helm, Then neither Workflows nor Temporal Workflows appears in sidebar

## Reference

- Design: `specs/temporal-workflows-ui/design.md`
- Plan: `specs/temporal-workflows-ui/plan.md`
- Kanban MCP reference: `go/plugins/kanban-mcp/` (follow same patterns exactly)
