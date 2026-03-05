# Implementation Plan: MCP Kanban Server

**Design:** `specs/mcp-kanban-server/design.md` (v1.2)
**Target directory:** `go/cmd/kanban-mcp/`

---

## Checklist

- [ ] Step 1: Project scaffold and configuration
- [ ] Step 2: Database layer (models + manager)
- [ ] Step 3: TaskService — core CRUD operations
- [ ] Step 4: TaskService — assign and subtask operations
- [ ] Step 5: TaskService — attachment operations
- [ ] Step 6: SSE Hub + TaskService broadcast integration
- [ ] Step 7: MCP server and all 12 tools (stdio transport)
- [ ] Step 8: HTTP server — full route wiring + MCP HTTP transport
- [ ] Step 9: REST API handlers
- [ ] Step 10: Embedded single-page Kanban UI with card detail view
- [ ] Step 11: Postgres support verification + Helm chart

---

## Step 1: Project Scaffold and Configuration

**Objective:** Create the directory structure, wire the binary into the existing Go module, and implement flag/env-based configuration. The binary must build and print usage.

**Implementation guidance:**

Create the following files:

```
go/cmd/kanban-mcp/
├── main.go
└── internal/
    └── config/
        └── config.go
```

`config.go` defines:
```go
type Config struct {
    Addr      string       // --addr / KANBAN_ADDR, default ":8080"
    Transport string       // --transport / KANBAN_TRANSPORT, "http" | "stdio"
    DBType    db.DBType    // --db-type / KANBAN_DB_TYPE, "sqlite" | "postgres"
    DBPath    string       // --db-path / KANBAN_DB_PATH, default "./kanban.db"
    DBURL     string       // --db-url / KANBAN_DB_URL
    LogLevel  string       // --log-level / KANBAN_LOG_LEVEL, default "info"
}

func Load() (*Config, error) // reads flags then falls back to env vars
```

`main.go` calls `config.Load()`, logs the resolved config, then exits cleanly.

Use `flag` stdlib package (no cobra — this is a single-purpose binary).
Env var fallback pattern: `if flag not set, check os.Getenv(envKey)`.

**Test requirements:**
- `TestLoad_Defaults`: verify all defaults are applied when no flags/env are set
- `TestLoad_EnvOverride`: set `KANBAN_ADDR=:9090`, verify Config.Addr is `:9090`

**Integration notes:** No dependencies on other steps. Uses only stdlib.

**Demo:** `go build ./cmd/kanban-mcp && ./kanban-mcp --help` prints all flags with defaults.

---

## Step 2: Database Layer

**Objective:** Define the `Task` and `Attachment` GORM models with all fields, implement the DB manager with SQLite/Postgres switching, and run AutoMigrate on startup.

**Implementation guidance:**

```
internal/db/
├── models.go    # Task, Attachment, TaskStatus, AttachmentType, StatusWorkflow, ValidStatus()
└── manager.go   # Manager struct, NewManager(), AutoMigrate(), DB() accessor
```

`models.go`:
```go
type TaskStatus string

const (
    StatusInbox      TaskStatus = "Inbox"
    StatusPlan       TaskStatus = "Plan"
    StatusDevelop    TaskStatus = "Develop"
    StatusTesting    TaskStatus = "Testing"
    StatusCodeReview TaskStatus = "CodeReview"
    StatusRelease    TaskStatus = "Release"
    StatusDone       TaskStatus = "Done"
)

var StatusWorkflow = []TaskStatus{ /* ordered */ }

func ValidStatus(s TaskStatus) bool

type Task struct {
    ID              uint
    Title           string
    Description     string
    Status          TaskStatus
    Assignee        string
    Labels          []string      `gorm:"serializer:json"`
    UserInputNeeded bool
    ParentID        *uint
    Subtasks        []*Task       `gorm:"foreignKey:ParentID"`
    Attachments     []*Attachment `gorm:"foreignKey:TaskID"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type AttachmentType string

const (
    AttachmentTypeFile AttachmentType = "file"
    AttachmentTypeLink AttachmentType = "link"
)

type Attachment struct {
    ID        uint
    TaskID    uint           `gorm:"not null;index"`
    Type      AttachmentType `gorm:"type:varchar(16);not null"`
    Filename  string         `gorm:"type:varchar(255)"`  // type=file
    Content   string         `gorm:"type:text"`           // type=file
    URL       string         `gorm:"type:text"`           // type=link
    Title     string         `gorm:"type:varchar(255)"`   // type=link (optional)
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

`manager.go` mirrors the pattern in `go/internal/database/manager.go`:
- Switch on `DBType` to open `github.com/glebarez/sqlite` or `gorm.io/driver/postgres`
- `TranslateError: true` in both cases
- `AutoMigrate(&Task{}, &Attachment{})` in `Initialize()`

**Test requirements:**
- `TestValidStatus`: table-driven; all 7 statuses valid, `""` and `"invalid"` not valid
- `TestNewManager_Sqlite`: open in-memory SQLite (`file::memory:?cache=shared`), call `AutoMigrate`, verify both `tasks` and `attachments` tables exist via `db.Migrator().HasTable()`
- `TestNewManager_InvalidType`: expect error for unknown DB type

**Integration notes:** `main.go` calls `db.NewManager(cfg)` then `manager.Initialize()` before serving.

**Demo:** Server starts with `--db-type=sqlite --db-path=/tmp/kanban.db`, logs "database initialized", file is created.

---

## Step 3: TaskService — Core CRUD Operations

**Objective:** Implement `TaskService` with `ListTasks`, `GetTask`, `CreateTask`, `UpdateTask`, `MoveTask`, `DeleteTask`. All mutations must call `hub.Broadcast()` — wire a no-op hub stub at this step.

**Implementation guidance:**

```
internal/service/
└── task_service.go
```

```go
type TaskFilter struct {
    Status   *TaskStatus
    Assignee *string
    Label    *string     // nil = all labels; set to filter tasks containing this label
    ParentID *uint       // nil = top-level only (WHERE parent_id IS NULL)
}

type CreateTaskRequest struct {
    Title       string
    Description string
    Status      TaskStatus // defaults to StatusInbox if empty
    Labels      []string
}

type UpdateTaskRequest struct {
    Title           *string
    Description     *string
    Status          *TaskStatus
    Assignee        *string
    Labels          *[]string   // nil = no change; non-nil replaces existing labels
    UserInputNeeded *bool
}

type Broadcaster interface {
    Broadcast(event interface{})
}

type TaskService struct {
    db          *gorm.DB
    broadcaster Broadcaster
}

func NewTaskService(db *gorm.DB, b Broadcaster) *TaskService
```

Key implementation notes:
- `ListTasks` with `ParentID == nil` appends `WHERE parent_id IS NULL` (top-level only by default)
- `MoveTask` validates status with `ValidStatus()` before updating; returns error for invalid status
- `DeleteTask` deletes attachments first (`WHERE task_id = ?`), then subtasks (`WHERE parent_id = ?`), then parent
- All mutations call `b.Broadcast(updatedBoard)` after DB write

**Test requirements (table-driven, in-memory SQLite):**
- `TestCreateTask_Defaults`: no status provided → status is `Inbox`
- `TestCreateTask_WithStatus`: status `Plan` persisted correctly
- `TestCreateTask_WithLabels`: labels persisted and returned
- `TestGetTask_NotFound`: returns wrapped sentinel error
- `TestMoveTask_Valid`: status updated in DB
- `TestMoveTask_InvalidStatus`: returns error without DB write
- `TestListTasks_Filter`: create 3 tasks across 2 statuses, filter by status returns correct subset
- `TestListTasks_LabelFilter`: create tasks with labels, filter by label returns correct subset
- `TestDeleteTask_Simple`: task is deleted, subsequent GetTask returns not-found
- `TestBroadcast_CalledOnMutation`: mock Broadcaster, verify `Broadcast` called once per mutation

**Integration notes:** `Broadcaster` interface keeps TaskService decoupled from SSE Hub (injected later in Step 6).

**Demo:** Unit tests pass: `go test ./cmd/kanban-mcp/internal/service/...`

---

## Step 4: TaskService — Assign and Subtask Operations

**Objective:** Add `AssignTask` and `CreateSubtask` to TaskService, enforce the one-level nesting constraint, and implement cascade delete for subtasks.

**Implementation guidance:**

Add to `task_service.go`:

```go
func (s *TaskService) AssignTask(ctx context.Context, id uint, assignee string) (*Task, error)

func (s *TaskService) CreateSubtask(ctx context.Context, parentID uint, req CreateTaskRequest) (*Task, error)
```

`AssignTask`: GORM update of `assignee` column; empty string is valid (clears assignment).

`CreateSubtask` constraints:
1. Fetch parent; return error if not found
2. Reject if `parent.ParentID != nil` → return `fmt.Errorf("subtasks cannot have subtasks")`
3. Insert new Task with `ParentID = &parentID`
4. Broadcast after insert

`DeleteTask` update — explicit cascade (works in both SQLite and Postgres without relying on DB-level cascade):
```go
func (s *TaskService) DeleteTask(ctx context.Context, id uint) error {
    // 1. Verify task exists
    // 2. Delete attachments: db.Where("task_id = ?", id).Delete(&Attachment{})
    // 3. Delete subtasks: db.Where("parent_id = ?", id).Delete(&Task{})
    // 4. Delete parent
    // 5. Broadcast
}
```

`GetTask` must eager-load subtasks and attachments: `db.Preload("Subtasks").Preload("Attachments").First(&task, id)`

**Test requirements:**
- `TestAssignTask`: assign, verify DB; reassign, verify updated; clear with `""`, verify `assignee = ""`
- `TestListTasks_AssigneeFilter`: create tasks for "alice" and "bob"; filter returns only alice's
- `TestCreateSubtask_Valid`: parent exists, subtask created with correct ParentID
- `TestCreateSubtask_ParentNotFound`: non-existent parentID returns error
- `TestCreateSubtask_NestedRejection`: try to create subtask of a subtask → error "subtasks cannot have subtasks"
- `TestDeleteTask_Cascade`: parent + 2 subtasks created; delete parent; all 3 gone from DB
- `TestGetTask_WithSubtasks`: get parent task; subtasks populated in result

**Integration notes:** Task and subtask operations complete. Step 5 adds attachment operations.

**Demo:** Unit tests pass: `go test ./cmd/kanban-mcp/internal/service/... -run TestSubtask -run TestAssign`

---

## Step 5: TaskService — Attachment Operations

**Objective:** Add `AddAttachment` and `DeleteAttachment` to TaskService, enforce top-level-only constraint, validate attachment types, and verify cascade delete includes attachments.

**Implementation guidance:**

Add to `task_service.go`:

```go
type CreateAttachmentRequest struct {
    Type     AttachmentType // "file" or "link"
    Filename string         // required for type=file
    Content  string         // required for type=file
    URL      string         // required for type=link
    Title    string         // optional for type=link
}

func (s *TaskService) AddAttachment(ctx context.Context, taskID uint, req CreateAttachmentRequest) (*Attachment, error)

func (s *TaskService) DeleteAttachment(ctx context.Context, id uint) error
```

`AddAttachment` constraints:
1. Fetch task by `taskID`; return error if not found
2. Reject if `task.ParentID != nil` → return `fmt.Errorf("attachments can only be added to top-level tasks")`
3. Validate type: must be `"file"` or `"link"`; return error listing valid types if invalid
4. For `type=file`: require `filename` and `content` non-empty
5. For `type=link`: require `url` non-empty
6. Insert `Attachment` with `TaskID = taskID`
7. Broadcast after insert

`DeleteAttachment`:
1. Fetch attachment by ID; return error if not found
2. Delete attachment
3. Broadcast after delete

`GetTask` and `GetBoard` already eager-load `Attachments` from Step 4.

**Test requirements (table-driven, in-memory SQLite):**
- `TestAddAttachment_File`: add file attachment with filename="DESIGN.md" + content → persisted correctly
- `TestAddAttachment_Link`: add link attachment with url + title → persisted correctly
- `TestAddAttachment_SubtaskRejected`: attempt to add attachment to subtask → error "attachments can only be added to top-level tasks"
- `TestAddAttachment_TaskNotFound`: non-existent taskID → error
- `TestAddAttachment_InvalidType`: type="invalid" → error listing valid types
- `TestAddAttachment_FileMissingFields`: type="file" with empty filename → error; empty content → error
- `TestAddAttachment_LinkMissingURL`: type="link" with empty url → error
- `TestDeleteAttachment_Valid`: attachment deleted from DB
- `TestDeleteAttachment_NotFound`: non-existent ID → error
- `TestDeleteTask_CascadeWithAttachments`: parent task + 2 attachments + 1 subtask → delete parent → all gone
- `TestGetTask_WithAttachments`: get task; attachments populated in result
- `TestBroadcast_CalledOnAttachmentMutation`: verify Broadcast called for add and delete

**Integration notes:** All 12 `TaskService` operations are now complete. Step 6 replaces the stub Broadcaster.

**Demo:** Unit tests pass: `go test ./cmd/kanban-mcp/internal/service/... -run TestAttachment`

---

## Step 6: SSE Hub + Broadcast Integration

**Objective:** Implement the SSE Hub, integrate it as the `Broadcaster` in TaskService, and add the `/events` HTTP endpoint. After any task mutation (including attachment add/delete), all connected SSE clients receive a `board_update` event with the full board state.

**Implementation guidance:**

```
internal/sse/
└── hub.go
```

```go
type Event struct {
    Type string      `json:"type"` // always "board_update" in v1
    Data interface{} `json:"data"`
}

type Hub struct {
    mu   sync.RWMutex
    subs map[chan Event]struct{}
}

func NewHub() *Hub
func (h *Hub) Subscribe() chan Event
func (h *Hub) Unsubscribe(ch chan Event)
func (h *Hub) Broadcast(event interface{})  // implements service.Broadcaster

func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request)
```

`ServeSSE`:
1. Set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`
2. Assert `http.Flusher`; return 500 if not supported
3. `ch := h.Subscribe(); defer h.Unsubscribe(ch)`
4. Send initial snapshot: `fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", boardJSON); flusher.Flush()`
5. Loop on `r.Context().Done()` vs `ch` message

`TaskService.Broadcast` implementation: marshal the full board (all tasks grouped by status, with subtasks and attachments) and call `hub.Broadcast(Event{Type: "board_update", Data: board})`.

**Test requirements:**
- `TestHub_SubscribeUnsubscribe`: subscribe 3 clients, unsubscribe 1, broadcast → 2 receive, 1 does not
- `TestHub_Broadcast_NonBlocking`: slow subscriber (full channel buffer) does not block other subscribers
- `TestHub_ConcurrentSubscribers`: 50 goroutines subscribe concurrently, broadcast once, all 50 receive
- `TestServeSSE_Integration`: use `httptest.NewRecorder`, connect SSE client, trigger mutation, verify event received

**Integration notes:** `Hub` is constructed in `main.go` and injected into `TaskService`. `ServeSSE` is registered as a route in Step 8.

**Demo:** Unit tests pass. Manually: start partial server (Step 8 incomplete), verify `/events` endpoint returns SSE stream.

---

## Step 7: MCP Server and All 12 Tools (stdio Transport)

**Objective:** Create the MCP server, register all 12 tools (10 task + 2 attachment), and verify end-to-end tool calls work over stdio transport.

**Implementation guidance:**

```
internal/mcp/
└── tools.go
```

Pattern mirrors `go/internal/mcp/mcp_handler.go`:

```go
func NewServer(svc *service.TaskService) *mcp.Server {
    server := mcp.NewServer(&mcp.Implementation{
        Name:    "kanban",
        Version: "v1.0.0",
    }, nil)

    // Task tools (10)
    mcp.AddTool(server, &mcp.Tool{Name: "list_tasks",   Description: "..."}, handleListTasks(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "get_task",     Description: "..."}, handleGetTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "create_task",  Description: "..."}, handleCreateTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "create_subtask", Description: "..."}, handleCreateSubtask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "assign_task",  Description: "..."}, handleAssignTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "move_task",    Description: "..."}, handleMoveTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "update_task",  Description: "..."}, handleUpdateTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "set_user_input_needed", Description: "..."}, handleSetUserInputNeeded(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "delete_task",  Description: "..."}, handleDeleteTask(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "get_board",    Description: "..."}, handleGetBoard(svc))

    // Attachment tools (2)
    mcp.AddTool(server, &mcp.Tool{Name: "add_attachment",    Description: "..."}, handleAddAttachment(svc))
    mcp.AddTool(server, &mcp.Tool{Name: "delete_attachment", Description: "..."}, handleDeleteAttachment(svc))

    return server
}
```

Each handler follows: parse typed input → call `svc` method → return result or `isError: true` on error.

`add_attachment` input fields: `task_id` (int), `type` ("file"|"link"), `filename?`, `content?`, `url?`, `title?`
`delete_attachment` input fields: `id` (int)

`main.go` stdio mode:
```go
if cfg.Transport == "stdio" {
    server.Run(ctx, mcp.NewStdioTransport())
    return
}
```

**Test requirements (use `mcp.NewInMemoryTransports()`):**
- `TestMCPTool_CreateTask`: call `create_task`, verify task returned with correct fields
- `TestMCPTool_MoveTask_Invalid`: call `move_task` with bad status → `isError: true` in result
- `TestMCPTool_CreateSubtask`: call `create_task` then `create_subtask` → subtask has correct parent_id
- `TestMCPTool_AssignTask`: call `assign_task` → returned task has assignee set
- `TestMCPTool_DeleteTask_Cascade`: create parent + subtask + attachment via MCP, delete parent, `get_task` returns error
- `TestMCPTool_GetBoard`: create tasks in 3 statuses, `get_board` returns all columns with attachments
- `TestMCPTool_AddAttachment_File`: call `add_attachment` with type="file" → attachment returned
- `TestMCPTool_AddAttachment_Link`: call `add_attachment` with type="link" → attachment returned
- `TestMCPTool_AddAttachment_SubtaskRejected`: add attachment to subtask → `isError: true`
- `TestMCPTool_DeleteAttachment`: add then delete attachment → `success: true`

**Integration notes:** stdio path is fully functional after this step — kagent can register this binary as an MCP server via stdio.

**Demo:**
```bash
./kanban-mcp --transport=stdio <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Test"}}}
EOF
# Returns: {"result":{"content":[{"type":"text","text":"{\"id\":1,\"title\":\"Test\",...}"}]}}
```

---

## Step 8: HTTP Server — Full Route Wiring + MCP HTTP Transport

**Objective:** Build the HTTP server that mounts all four surfaces on one port and switches between stdio and HTTP transport at startup.

**Implementation guidance:**

`main.go` (HTTP mode):
```go
mcpServer := mcp.NewMCPServer(svc)
mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
    return mcpServer
}, nil)

mux := http.NewServeMux()
mux.Handle("/mcp",                mcpHandler)
mux.HandleFunc("/events",         hub.ServeSSE)
mux.HandleFunc("/api/tasks",      api.TasksHandler(svc))
mux.HandleFunc("/api/tasks/",     api.TaskHandler(svc))       // /:id, /:id/subtasks, /:id/attachments
mux.HandleFunc("/api/attachments/", api.AttachmentHandler(svc)) // /api/attachments/:id (DELETE)
mux.HandleFunc("/api/board",      api.BoardHandler(svc))
mux.Handle("/",                   ui.Handler())                // embedded SPA (Step 10)

log.Printf("kanban-mcp listening on %s", cfg.Addr)
http.ListenAndServe(cfg.Addr, mux)
```

Extract server construction to `server.go` alongside `main.go` for testability:
```go
func NewHTTPServer(cfg *config.Config, svc *service.TaskService, hub *sse.Hub) *http.Server
```

**Test requirements:**
- `TestHTTPServer_MCP`: `httptest.NewServer`, call `/mcp` with a valid MCP request, verify 200 + valid JSON-RPC response
- `TestHTTPServer_SSE`: connect to `/events`, verify `Content-Type: text/event-stream` and initial snapshot event
- `TestHTTPServer_NotFound`: GET `/api/tasks/99999` returns 404
- `TestHTTPServer_CORS`: requests to `/mcp` include expected headers

**Integration notes:** `/api/*` handlers return 501 Not Implemented stubs until Step 9. `/` returns 404 stub until Step 10. MCP over HTTP is fully functional after this step.

**Demo:**
```bash
./kanban-mcp &
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_board","arguments":{}}}'
```

---

## Step 9: REST API Handlers

**Objective:** Implement all REST endpoints including attachment routes, replace the 501 stubs from Step 8.

**Implementation guidance:**

```
internal/api/
└── handlers.go
```

Route dispatch using URL path inspection in `net/http` (no external router):
```go
// TasksHandler handles /api/tasks (GET list, POST create)
// TaskHandler handles /api/tasks/{id} (GET, PUT, DELETE),
//   /api/tasks/{id}/subtasks (GET, POST), and /api/tasks/{id}/attachments (POST)
// AttachmentHandler handles /api/attachments/{id} (DELETE)
// BoardHandler handles /api/board (GET)
```

Parse `id` from URL path with `strings.TrimPrefix` + `strconv.Atoi`.

Detect sub-routes: `strings.HasSuffix(r.URL.Path, "/subtasks")` and `/attachments"`.

Response helpers:
```go
func writeJSON(w http.ResponseWriter, status int, v interface{})
func writeError(w http.ResponseWriter, status int, msg string)
```

Error → HTTP status mapping:
- `gorm.ErrRecordNotFound` → 404
- validation errors (invalid status, nesting, attachment type) → 400
- all others → 500

**Test requirements (httptest.NewServer with real in-memory SQLite):**
- `TestREST_CreateTask`: POST `/api/tasks` → 201 + task JSON
- `TestREST_GetTask`: GET `/api/tasks/1` → 200; GET `/api/tasks/999` → 404
- `TestREST_UpdateTask`: PUT `/api/tasks/1` with `{"status":"Plan"}` → 200 + updated task
- `TestREST_ListTasks_Filter`: GET `/api/tasks?status=Inbox` returns filtered list
- `TestREST_Subtasks_Create`: POST `/api/tasks/1/subtasks` → 201 + subtask JSON
- `TestREST_Subtasks_List`: GET `/api/tasks/1/subtasks` → 200 + subtask array
- `TestREST_DeleteTask_Cascade`: DELETE `/api/tasks/1` → 204; subtasks and attachments also gone
- `TestREST_Board`: GET `/api/board` → 200 + columns with subtasks and attachments inline
- `TestREST_AddAttachment_File`: POST `/api/tasks/1/attachments` with type="file" → 201 + attachment JSON
- `TestREST_AddAttachment_Link`: POST `/api/tasks/1/attachments` with type="link" → 201 + attachment JSON
- `TestREST_AddAttachment_SubtaskRejected`: POST `/api/tasks/{subtask_id}/attachments` → 400
- `TestREST_DeleteAttachment`: DELETE `/api/attachments/1` → 204
- `TestREST_DeleteAttachment_NotFound`: DELETE `/api/attachments/999` → 404
- `TestREST_SSE_AfterMutation`: connect SSE, POST task, verify board_update event received

**Integration notes:** SSE Hub is already wired from Step 6; every REST mutation (including attachment operations) auto-broadcasts. All API surfaces are now independently functional.

**Demo:**
```bash
# Create task
curl -s -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Ship it","status":"Inbox"}' | jq .

# Add file attachment
curl -s -X POST http://localhost:8080/api/tasks/1/attachments \
  -H "Content-Type: application/json" \
  -d '{"type":"file","filename":"DESIGN.md","content":"# Design\n\nOverview..."}' | jq .

# Add link attachment
curl -s -X POST http://localhost:8080/api/tasks/1/attachments \
  -H "Content-Type: application/json" \
  -d '{"type":"link","url":"https://claude.ai/session/abc","title":"Agent Session"}' | jq .

# Get task with attachments
curl -s http://localhost:8080/api/tasks/1 | jq '.attachments'

# Get board
curl -s http://localhost:8080/api/board | jq '.columns[0]'
```

---

## Step 10: Embedded Single-Page Kanban UI with Card Detail View

**Objective:** Create `internal/ui/index.html`, embed it in the binary, wire it to the `/` route, connect the browser to `/events` for real-time updates, and implement a card detail view with attachment rendering.

**Implementation guidance:**

```
internal/ui/
├── embed.go      # //go:embed index.html + Handler()
└── index.html    # the full SPA
```

`embed.go`:
```go
package ui

import (
    _ "embed"
    "net/http"
)

//go:embed index.html
var indexHTML []byte

func Handler() http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write(indexHTML)
    })
}
```

`index.html` structure (vanilla JS, inline CSS, no build step):

```
<html>
  <head>
    <style> /* board layout: flex row, 7 columns, card styles, badge colors, detail modal */ </style>
  </head>
  <body>
    <div id="board"></div>
    <div id="detail-modal" class="hidden"></div>
    <script>
      // 1. fetch('/api/board') on load → renderBoard(data)
      // 2. EventSource('/events') → on 'snapshot' and 'message' → renderBoard(data)
      // 3. renderBoard(board): clear #board, render 7 columns in StatusWorkflow order
      // 4. renderCard(task): title, assignee badge (blue), HITL badge (amber),
      //    label chips, attachment icon + count (paperclip), subtask count
      //    - Subtasks: collapsible <details> with status pill per subtask
      //    - Buttons: ← Prev | Next → (call PUT /api/tasks/:id with next/prev status)
      //    - HITL toggle: calls PUT /api/tasks/:id with user_input_needed toggled
      //    - Click handler: opens detail modal
      // 5. renderDetailModal(task): full task info + attachments list
      //    - File attachments (.md): render as formatted HTML (simple markdown→HTML converter)
      //    - File attachments (.diff): render inside <pre><code> block
      //    - Other file types: render as <pre> plain text
      //    - Link attachments: render as clickable <a> with title (or URL as fallback)
      //    - Close button to dismiss modal and return to board
      // 6. Add-task form in Inbox column header: title input + Submit button
      //    - On submit: POST /api/tasks {title, status: "Inbox"} — SSE will trigger re-render
    </script>
  </body>
</html>
```

UI must handle the case where `/events` disconnects (browser's EventSource retries automatically).

**Test requirements:**
- `TestUI_Handler`: GET `/` returns 200 with `Content-Type: text/html` and non-empty body containing "Kanban"
- `TestUI_Embedded`: verify `indexHTML` is non-empty at init time (catches missing embed)
- Manual browser test: open `http://localhost:8080/`, verify all 7 columns, add a task, see it appear without reload
- Manual browser test: click a task card → detail modal opens showing full task info
- Manual browser test: add attachments via curl → card shows paperclip icon with count → click card → attachments visible in detail view with markdown rendered

**Integration notes:** Replace the `/` stub from Step 8 with `ui.Handler()`. All four surfaces now operational.

**Demo:** Open browser → `http://localhost:8080/` → Kanban board with live columns. In another terminal:
```bash
# Create task
curl -s -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello board"}'
# → card appears in Inbox immediately

# Add attachment
curl -s -X POST http://localhost:8080/api/tasks/1/attachments \
  -H "Content-Type: application/json" \
  -d '{"type":"file","filename":"NOTES.md","content":"# Notes\n\nSome markdown content"}'
# → card shows paperclip icon "1", click card → detail modal shows rendered markdown
```

---

## Step 11: Postgres Verification + Helm Chart

**Objective:** Verify the Postgres path end-to-end (including attachments), add a Dockerfile, and create a Helm chart in `contrib/tools/kanban-mcp/` for Kubernetes deployment.

**Implementation guidance:**

**Postgres verification:**
- Spin up Postgres in CI or locally: `docker run -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16`
- Run: `./kanban-mcp --db-type=postgres --db-url="host=localhost user=postgres password=test dbname=postgres port=5432 sslmode=disable"`
- Verify AutoMigrate creates both `tasks` and `attachments` tables, all REST and MCP operations work including attachment CRUD

**Dockerfile** (`go/cmd/kanban-mcp/Dockerfile`):
```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN go build -o kanban-mcp ./cmd/kanban-mcp

FROM alpine:3.20
COPY --from=builder /app/go/kanban-mcp /usr/local/bin/kanban-mcp
ENTRYPOINT ["kanban-mcp"]
```

**Helm chart** (`contrib/tools/kanban-mcp/`):
```
contrib/tools/kanban-mcp/
├── Chart.yaml
├── values.yaml          # addr, transport, dbType, dbPath, dbUrl, logLevel
└── templates/
    ├── deployment.yaml
    ├── service.yaml     # ClusterIP :8080
    └── _helpers.tpl
```

`values.yaml` defaults:
```yaml
image:
  repository: ghcr.io/kagent-dev/kanban-mcp
  tag: latest
config:
  addr: ":8080"
  transport: "http"
  dbType: "sqlite"
  dbPath: "/data/kanban.db"
persistence:
  enabled: true
  size: 1Gi
```

For Postgres: `config.dbType: "postgres"` + `config.dbUrl` (can reference a Secret via `valueFrom`).

**Test requirements:**
- `TestPostgres_Integration` (skipped unless `KANBAN_TEST_POSTGRES_URL` set): run full CRUD + subtask + assign + attachment workflow against real Postgres
- `helm lint contrib/tools/kanban-mcp/` passes
- `helm template test contrib/tools/kanban-mcp/` produces valid K8s manifests

**Integration notes:** After this step, the server can be registered in kagent as a `RemoteMCPServer` CRD pointing to the in-cluster Service.

**Demo:**
```bash
# Deploy to local Kind cluster
helm install kanban-mcp contrib/tools/kanban-mcp/ -n kagent
kubectl port-forward -n kagent svc/kanban-mcp 8080:8080

# Register in kagent
kubectl apply -f - <<EOF
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: kanban
  namespace: kagent
spec:
  url: http://kanban-mcp.kagent.svc:8080/mcp
EOF
```

---

## Implementation Order Notes

- Steps 1–6 are pure Go with no external HTTP concerns; safe to develop and test in isolation.
- Step 5 (attachments) is self-contained and only depends on the Task model from Step 2 and TaskService from Steps 3–4.
- Step 7 gives a fully working MCP server (stdio) with all 12 tools that kagent can use immediately.
- Step 8 unlocks HTTP transport and SSE in one pass.
- Steps 9–10 add the human-facing surfaces without touching core logic.
- Step 10 introduces the card detail view — the only UI surface where attachments are fully visible.
- Step 11 is independently releasable; the binary is already functional without the Helm chart.
