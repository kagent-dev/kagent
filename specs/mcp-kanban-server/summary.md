# Summary: MCP Kanban Server

**Spec directory:** `specs/mcp-kanban-server/`
**Design version:** 1.2
**Date:** 2026-02-25

---

## Artifacts

| File | Description |
|------|-------------|
| `rough-idea.md` | Original idea + elaborated requirements |
| `requirements.md` | Q&A record including v1.2 attachment requirements |
| `research/r1-go-mcp-sdk.md` | Official Go MCP SDK patterns |
| `research/r2-kagent-mcp-structure.md` | Kagent MCP structure and prior art |
| `research/r3-gorm-dual-support.md` | GORM SQLite/Postgres dual support pattern |
| `research/r4-realtime-ui.md` | SSE vs WebSocket analysis and recommendation |
| `research/r5-existing-kanban-mcp.md` | Prior art: eyalzh/kanban-mcp, bradrisse/kanban-mcp |
| `design.md` | Full design document (v1.2) |
| `plan.md` | 11-step incremental implementation plan |
| `PROMPT.md` | Ralph prompt for autonomous implementation |

---

## What Is Being Built

A **self-contained Go binary** (`go/cmd/kanban-mcp/`) that serves four surfaces on a single port:

1. **MCP Server** — 12 tools for AI agent task management via Model Context Protocol
2. **REST API** — CRUD endpoints for tasks, subtasks, attachments, and board state
3. **SSE endpoint** — real-time push to browser clients after every mutation
4. **Embedded SPA** — single-page vanilla HTML+JS Kanban board with card detail view, no build step

### MCP Tools (12 total)

`list_tasks` · `get_task` · `create_task` · `create_subtask` · `assign_task` · `move_task` · `update_task` · `set_user_input_needed` · `delete_task` · `get_board` · `add_attachment` · `delete_attachment`

### Task Model

```
Task {
  id, title, description,
  status (Inbox|Plan|Develop|Testing|CodeReview|Release|Done),
  assignee,           // free-form string; filter support in list_tasks
  labels[],           // free-form strings; case-insensitive filtering
  user_input_needed,  // Human-in-the-Loop flag; amber badge in UI
  parent_id,          // nil = top-level; set = subtask (1 level deep)
  subtasks[],         // eager-loaded in get_task / get_board; rendered inline on cards
  attachments[],      // top-level tasks only; eager-loaded in get_task / get_board
  created_at, updated_at
}
```

### Attachment Model

```
Attachment {
  id, task_id,
  type,       // "file" or "link"
  filename,   // type=file: e.g. "DESIGN.md", "CHANGES.diff"
  content,    // type=file: full text stored as TEXT in DB
  url,        // type=link: external URL
  title,      // type=link: optional display title
  created_at, updated_at
}
```

---

## Key Design Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| MCP SDK | `modelcontextprotocol/go-sdk` | Already in go.mod; consistent with kagent |
| SQLite driver | `github.com/glebarez/sqlite` | Already in go.mod; pure Go, no CGO |
| Postgres | `gorm.io/driver/postgres` | Already in go.mod |
| Real-time | SSE (stdlib only) | Zero dependencies; browser auto-reconnects |
| UI | Vanilla HTML+JS, embedded | No build step; single binary |
| Attachment storage | DB TEXT column | Simple; no disk/volume needed for text files |
| Attachment tools | Add + delete only | Minimal surface; no update (delete + re-add) |
| New dependencies | **None** | All libs already in go/go.mod |

---

## Suggested Next Steps

1. **Implement** — run `ralph run` with the plan, or work through `plan.md` step by step
2. **Register in kagent** — after Step 7, register the binary as a stdio MCP server in kagent
3. **Deploy to cluster** — after Step 11, use the Helm chart to deploy and register as a `RemoteMCPServer`
4. **Auth** — add token-based auth to `/mcp` and `/api/*` if exposed outside the cluster

---

## Ralph Integration

To implement autonomously, use the `PROMPT.md` and run:

```bash
# Full pipeline
ralph run --config presets/pdd-to-code-assist.yml

# Simpler spec-driven flow
ralph run --config presets/spec-driven.yml
```
