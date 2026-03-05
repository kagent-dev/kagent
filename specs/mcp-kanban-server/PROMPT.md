# MCP Kanban Server

## Objective

Build a self-contained Go binary at `go/cmd/kanban-mcp/` that implements an MCP server
for Kanban task management with attachment support. Full spec in `specs/mcp-kanban-server/design.md`.
Follow the 11-step plan in `specs/mcp-kanban-server/plan.md` in order.

## Constraints

- Use ONLY dependencies already in `go/go.mod` — no new dependencies
- MCP SDK: `github.com/modelcontextprotocol/go-sdk` (already present)
- DB: GORM with `github.com/glebarez/sqlite` (default) and `gorm.io/driver/postgres`
- Real-time: SSE via stdlib `net/http` only — no WebSocket library
- UI: single `internal/ui/index.html` embedded with `//go:embed` — no npm, no build step
- Follow kagent Go conventions from `CLAUDE.md`: wrap errors with `%w`, table-driven tests

## Key Requirements

- **12 MCP tools:** `list_tasks`, `get_task`, `create_task`, `create_subtask`, `assign_task`,
  `move_task`, `update_task`, `set_user_input_needed`, `delete_task`, `get_board`,
  `add_attachment`, `delete_attachment`
- **Task statuses (enum):** `Inbox → Plan → Develop → Testing → CodeReview → Release → Done`
- **Task fields:** `id`, `title`, `description`, `status`, `assignee`, `labels[]`, `user_input_needed` (bool), `parent_id` (nullable), `subtasks[]`, `attachments[]`
- **Attachment model:** `id`, `task_id`, `type` (file|link), `filename`, `content` (TEXT), `url`, `title`
- **Attachment rules:** top-level tasks only; type=file requires filename+content; type=link requires url; cascade delete with task
- **Subtask rules:** one level deep only; no attachments on subtasks; `delete_task` cascades to subtasks and attachments
- **UI:** card view shows paperclip icon + count; click card opens detail modal with attachments (markdown rendered, diffs as code blocks, links clickable)
- **Transports:** `--transport=stdio` (default) or `--transport=http`
- **Single port:** `/mcp`, `/events` (SSE), `/api/tasks`, `/api/tasks/:id/attachments`, `/api/attachments/:id`, `/api/board`, `/` (SPA)
- **Config:** all settings via CLI flags with `KANBAN_*` env var fallback

## Acceptance Criteria

```
Given: MCP server running in HTTP mode
When:  create_task called with title="Fix bug", status="Plan"
Then:  task persisted; SSE clients receive board_update within 100ms

Given: task exists with status="Develop"
When:  move_task called with status="INVALID"
Then:  tool returns isError:true listing valid statuses

Given: top-level task id=5 exists
When:  create_subtask called with parent_id=5
Then:  subtask created with parent_id=5; get_task(5) returns subtasks populated
And:   UI renders the subtask inline on the parent card

Given: task id=3 has 2 subtasks and 3 attachments
When:  delete_task called with id=3
Then:  task, both subtasks, and all 3 attachments deleted from DB

Given: top-level task id=7 exists
When:  add_attachment called with task_id=7, type="file", filename="DESIGN.md", content="# Design..."
Then:  attachment persisted; get_task(7) includes attachment; SSE board_update sent

Given: top-level task id=7 exists
When:  add_attachment called with task_id=7, type="link", url="https://example.com", title="Ref"
Then:  link attachment persisted; get_board shows attachment count on task

Given: subtask id=8 exists (parent_id=7)
When:  add_attachment called with task_id=8
Then:  error returned: "attachments can only be added to top-level tasks"

Given: attachment id=20 exists
When:  delete_attachment called with id=20
Then:  attachment deleted; SSE board_update sent

Given: task exists with user_input_needed=false
When:  set_user_input_needed called with needed=true
Then:  flag persisted; UI renders amber badge on card

Given: browser connected to /events
When:  any mutation occurs via MCP or REST
Then:  board_update SSE event received; UI re-renders without page reload

Given: browser GETs /
When:  board is rendered
Then:  all 7 status columns visible; cards show paperclip icon if attachments exist

Given: user clicks a task card in the UI
When:  detail modal opens
Then:  .md attachments rendered as formatted markdown, .diff as code blocks, links as clickable URLs

Given: server started with --transport=stdio
When:  MCP client connects via stdin/stdout
Then:  all 12 tools function identically to HTTP mode

Given: server started with --db-type=postgres --db-url=<DSN>
When:  full CRUD including attachments performed
Then:  all operations succeed against Postgres
```

## References

- Full design: `specs/mcp-kanban-server/design.md`
- Step-by-step plan: `specs/mcp-kanban-server/plan.md`
- Kagent MCP pattern to follow: `go/internal/mcp/mcp_handler.go`
- DB manager pattern to follow: `go/internal/database/manager.go`
- Go MCP scaffold template: `go/cli/internal/mcp/frameworks/golang/templates/`
- Existing Helm tool charts: `contrib/tools/`
