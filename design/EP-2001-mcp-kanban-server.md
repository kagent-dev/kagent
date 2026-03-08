# EP-2001: MCP Kanban Server

* Status: **Implemented**
* Spec: [specs/mcp-kanban-server](../specs/mcp-kanban-server/)

## Background

Self-contained Go binary (`go/plugins/kanban-mcp/`) serving four surfaces on a single port: MCP Server (12 tools), REST API (CRUD), SSE endpoint (real-time push), and embedded SPA (vanilla HTML+JS). Provides task management for AI agents with human-in-the-loop support.

## Motivation

Agents need a persistent task board to track work items across workflow stages. The kanban board enables both AI agents (via MCP tools) and humans (via web UI) to manage tasks collaboratively with real-time updates.

### Goals

- 12 MCP tools: list/get/create/update/delete tasks, subtasks, attachments, board view, HITL flag
- 7-stage workflow: Inbox → Plan → Develop → Testing → CodeReview → Release → Done
- Real-time SSE updates with zero external dependencies
- Embedded vanilla HTML+JS SPA with no build step
- Dual database support: SQLite (dev) / PostgreSQL (prod) via GORM

### Non-Goals

- Multi-board support
- Drag-and-drop UI
- Deep subtask nesting (1 level only)

## Implementation Details

- **Binary:** `go/plugins/kanban-mcp/`
- **Database:** GORM with Task + Attachment models, `//go:embed` for UI
- **Protocols:** MCP Streamable HTTP at `/mcp`, REST at `/api/*`, SSE at `/events`
- **Deployment:** Helm sub-chart, registered as RemoteMCPServer CRD with UI plugin metadata

### Test Plan

- Unit tests per package (`service/`, `api/`, `mcp/`, `sse/`, `config/`)
- Postgres integration test with `KANBAN_TEST_POSTGRES_URL`
