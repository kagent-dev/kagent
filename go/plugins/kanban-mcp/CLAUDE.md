# CLAUDE.md — kanban-mcp

Guide for AI agents working in the `go/cmd/kanban-mcp/` subtree.

## What This Is

A self-contained Go binary that provides a Kanban task board via three interfaces:
- **MCP** (Model Context Protocol) — 12 tools for AI agent integration (10 task + 2 attachment)
- **REST API** — CRUD endpoints for tasks, attachments, and board
- **Embedded SPA** — single HTML file served at `/`, with live SSE updates

## Project Layout

```
go/cmd/kanban-mcp/
├── main.go                          # Entry point, wires config → DB → service → server
├── server.go                        # HTTP mux: /mcp, /events, /api/*, /
├── Dockerfile
├── internal/
│   ├── config/config.go             # CLI flags + KANBAN_* env fallback
│   ├── db/
│   │   ├── models.go                # GORM Task + Attachment models + TaskStatus enum
│   │   └── manager.go               # DB init (SQLite or Postgres)
│   ├── service/task_service.go      # Business logic + Broadcaster interface
│   ├── mcp/tools.go                 # 12 MCP tool handlers (10 task + 2 attachment)
│   ├── api/handlers.go              # REST handlers (tasks, attachments, board)
│   ├── sse/hub.go                   # SSE fan-out hub (implements Broadcaster)
│   └── ui/
│       ├── embed.go                 # //go:embed index.html
│       ├── embed_test.go
│       └── index.html               # Full SPA — CSS + JS, no build step
```

## Critical: API JSON Field Naming

The `db.Task` GORM model uses Go PascalCase struct fields **without explicit JSON tags**, so the REST API and SSE events return **PascalCase** field names:

```json
{
  "ID": 1,
  "Title": "Fix bug",
  "Description": "Details here",
  "Status": "Develop",
  "Assignee": "alice",
  "Labels": ["priority:high", "team:platform"],
  "UserInputNeeded": false,
  "ParentID": null,
  "Subtasks": [{ "ID": 2, "Title": "Sub", ... }],
  "Attachments": [{ "ID": 1, "TaskID": 1, "Type": "file", "Filename": "DESIGN.md", ... }],
  "CreatedAt": "2026-02-25T17:32:38Z",
  "UpdatedAt": "2026-02-25T17:32:38Z"
}
```

The REST API **accepts** snake_case for write operations (via explicit `json:"..."` tags on handler input structs):
- POST/PUT body: `title`, `description`, `status`, `assignee`, `labels`, `user_input_needed`

**The UI `index.html` must normalize both casings.** A `norm()` function maps PascalCase → camelCase for rendering. If this breaks, cards show "(untitled)" and "#undefined".

## Board API Response Shape

`GET /api/board` and SSE `board_update` events both return:

```json
{
  "columns": [
    {
      "status": "Inbox",
      "tasks": [{ "ID": 1, "Title": "...", ... }]
    },
    {
      "status": "Design",
      "tasks": []
    }
  ]
}
```

- `columns[].status` is lowercase `json:"status"` (from `api.Column` / `mcp.Column` structs)
- `columns[].tasks[]` fields are PascalCase (from `db.Task` with no JSON tags)

## SSE Event Structure

SSE events at `/events` are wrapped in an `Event` envelope:

```json
{
  "type": "board_update",
  "data": { "columns": [...] }
}
```

- On connect: `event: snapshot\ndata: <last-broadcast-json>\n\n`
- On mutations: `data: <event-json>\n\n`

The UI must unwrap `ev.data.columns` from the parsed SSE payload.

## Workflow Statuses (Enum)

Exactly 8 statuses, in order:

```
Inbox → Design → Develop → Testing → SecurityScan → CodeReview → Documentation → Done
```

These are Go constants in `db/models.go` (`StatusInbox` through `StatusDone`). The UI mirrors this in the `WORKFLOW` array and provides human-readable labels via `COL_LABELS` (e.g., `SecurityScan` → "Security Scan").

## 12 MCP Tools

| Tool | Input | Description |
|------|-------|-------------|
| `list_tasks` | `status?`, `assignee?`, `label?` | List top-level tasks with optional filter |
| `get_task` | `id` | Get task by ID with subtasks + attachments populated |
| `create_task` | `title`, `description?`, `status?`, `labels?` | Create top-level task (defaults to Inbox) |
| `create_subtask` | `parent_id`, `title`, `description?`, `status?`, `labels?` | One level deep only |
| `assign_task` | `id`, `assignee` | Empty string clears assignment |
| `move_task` | `id`, `status` | Validates against enum |
| `update_task` | `id`, `title?`, `description?`, `status?`, `assignee?`, `labels?`, `user_input_needed?` | Partial update |
| `set_user_input_needed` | `id`, `needed` | Human-in-the-loop flag |
| `delete_task` | `id` | Cascades to subtasks + attachments |
| `get_board` | (none) | Full board grouped by columns with attachments |
| `add_attachment` | `task_id`, `type` (`file`\|`link`), `filename?`, `content?`, `url?`, `title?` | Add attachment to top-level task |
| `delete_attachment` | `id` | Delete an attachment by ID |

## REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/board` | Full board view |
| GET | `/api/tasks` | List tasks (`?status=`, `?assignee=`) |
| POST | `/api/tasks` | Create task (`{title, description?, status?}`) |
| GET | `/api/tasks/:id` | Get single task |
| PUT | `/api/tasks/:id` | Partial update (`{title?, description?, status?, assignee?, user_input_needed?}`) |
| DELETE | `/api/tasks/:id` | Delete task + subtasks + attachments |
| GET | `/api/tasks/:id/subtasks` | List subtasks |
| POST | `/api/tasks/:id/subtasks` | Create subtask |
| POST | `/api/tasks/:id/attachments` | Add attachment (top-level tasks only) |
| DELETE | `/api/attachments/:id` | Delete attachment by ID |
| GET | `/events` | SSE stream |
| * | `/mcp` | MCP Streamable HTTP endpoint |
| GET | `/` | Embedded SPA |

## Build & Run

```bash
# Build
cd go && go build -o kanban-mcp ./cmd/kanban-mcp/

# Run (HTTP mode, SQLite default)
./kanban-mcp
# → listening on :8080

# Run (stdio mode for MCP client piping)
./kanban-mcp --transport=stdio

# Run (Postgres)
./kanban-mcp --db-type=postgres --db-url="postgres://user:pass@host/db"
```

All flags have `KANBAN_*` environment variable fallbacks:
- `KANBAN_ADDR` (default `:8080`)
- `KANBAN_TRANSPORT` (default `http`)
- `KANBAN_DB_TYPE` (default `sqlite`)
- `KANBAN_DB_PATH` (default `./kanban.db`)
- `KANBAN_DB_URL`
- `KANBAN_LOG_LEVEL` (default `info`)

## UI Development

The UI is a **single embedded HTML file** at `internal/ui/index.html`. No npm, no build step. Changes require rebuilding the Go binary since the file is embedded via `//go:embed`.

Key UI architecture:
- Pure vanilla JS (no framework)
- CSS variables for theming
- SSE for live updates (reconnects automatically)
- `norm()` function normalizes PascalCase API fields to camelCase for rendering
- Column color coding via CSS classes (`.col-inbox`, `.col-design`, etc.)
- Cards show: title, description preview, ID badge, assignee badge, HITL flag, subtask count, label chips, attachment icon + count
- Click card opens detail modal with full task info + attachments (markdown rendered, diffs as code, links clickable)
- Navigation buttons show the target column name

## Testing

```bash
cd go

# Unit tests
go test ./cmd/kanban-mcp/...

# Specific package
go test ./cmd/kanban-mcp/internal/api/...
go test ./cmd/kanban-mcp/internal/mcp/...
go test ./cmd/kanban-mcp/internal/sse/...
go test ./cmd/kanban-mcp/internal/service/...
go test ./cmd/kanban-mcp/internal/config/...

# Postgres integration test (requires running Postgres)
KANBAN_TEST_POSTGRES_URL="postgres://..." go test ./cmd/kanban-mcp/internal/service/ -run TestPostgres -v
```

## Common Pitfalls

1. **PascalCase vs camelCase**: The GORM `Task` and `Attachment` models have no `json:"..."` tags, so API responses use Go field names (PascalCase). The UI's `norm()` function handles both. Any new fields added to `db.Task` or `db.Attachment` must also be mapped in `norm()`.

2. **Port already in use**: The binary exits immediately if `:8080` is occupied. Kill old processes: `kill -9 $(lsof -ti :8080)`.

3. **Stale UI after code change**: The HTML is `//go:embed`'d — you must `go build` again after editing `index.html`.

4. **Subtasks are one level deep**: `create_subtask` on a subtask returns an error. The `ParentID` field is `*uint` (nullable pointer).

5. **SSE snapshot vs stream**: New SSE subscribers get the last broadcast as a `snapshot` event, then receive `data` events for mutations. The UI handles both paths.
