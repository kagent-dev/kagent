# R5: Existing Kanban MCP Servers — Prior Art

## Notable Projects

### 1. eyalzh/kanban-mcp (TypeScript)
- Self-contained, SQLite-backed, designed for AI agent workflows
- Data model: Boards → Columns (with WIP limits) → Tasks (markdown content)
- GitHub: https://github.com/eyalzh/kanban-mcp

**Tool names:**
| Tool | Parameters |
|------|-----------|
| `create-kanban-board` | `name`, `projectGoal` |
| `add-task-to-board` | `boardId`, `title`, `content` |
| `move-task` | `taskId`, `targetColumnId`, `reason` |
| `delete-task` | `taskId` |
| `get-board-info` | `boardId` |
| `get-task-info` | `taskId` |
| `list-boards` | — |

### 2. bradrisse/kanban-mcp (TypeScript)
- Wraps Planka (external self-hosted app)
- More feature-rich: time tracking, checklists, comments
- GitHub: https://github.com/bradrisse/kanban-mcp

## Naming Convention Decision
Use **snake_case** for tool names (consistent with MCP Go SDK patterns in kagent):
- `list_tasks`, `create_task`, `update_task`, `move_task`, `delete_task`
- `list_boards`, `create_board`

## Key Differences for Our Implementation
- Single board per server (simpler, no `boardId` needed in every call)
- Fixed status workflow (not free-form columns): `Inbox → Design → Develop → Testing → SecurityScan → CodeReview → Documentation → Done`
- Status is an enum, not a configurable column — enforces workflow order
