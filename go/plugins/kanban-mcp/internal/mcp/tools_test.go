package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

// b64 base64-encodes a string for use as file attachment content.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// startPostgres starts a Postgres container, runs the kanban migrations, and
// returns a connection string. Tests skip when Docker is not available. This is a
// thin local copy of go/core/internal/dbtest (which cannot be imported across the
// internal/ boundary), mirroring the service-package test helper.
func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping container test")
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("kanban_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("kanban"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	if err := migrations.RunUp(connStr); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return connStr
}

// newTestClient starts Postgres, builds the MCP server over an in-memory
// transport, connects a client, and returns the connected client session.
func newTestClient(ctx context.Context, t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	url := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	svc := service.NewTaskService(dbgen.New(pool), pool, nil)
	server := NewServer(svc)

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

// callTool invokes a tool and decodes the structured output into out. It fails
// the test if the call reported isError.
func callTool(ctx context.Context, t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any, out any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned isError: %s", name, contentText(res))
	}
	if out != nil {
		decodeStructured(t, res, out)
	}
	return res
}

// decodeStructured re-marshals the structured tool output and unmarshals it into
// out, which keeps the assertions independent of the on-the-wire representation.
func decodeStructured(t *testing.T, res *mcpsdk.CallToolResult, out any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshaling structured content %s: %v", raw, err)
	}
}

// contentText concatenates the text content of a result for error messages.
func contentText(res *mcpsdk.CallToolResult) string {
	var s strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			s.WriteString(tc.Text)
		}
	}
	return s.String()
}

func TestMCPTool_CreateTask(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var out TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"title":  "Test task",
		"status": "Plan",
		"labels": []string{"alpha", "beta"},
	}, &out)

	if out.Task == nil {
		t.Fatal("create_task returned nil task")
	}
	if out.Task.Title != "Test task" {
		t.Errorf("title = %q, want %q", out.Task.Title, "Test task")
	}
	if out.Task.Status != "Plan" {
		t.Errorf("status = %q, want %q", out.Task.Status, "Plan")
	}
	if len(out.Task.Labels) != 2 {
		t.Errorf("labels = %v, want 2 labels", out.Task.Labels)
	}
}

func TestMCPTool_MoveTask_Invalid(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var created TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Moveable"}, &created)

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "move_task",
		Arguments: map[string]any{"id": created.Task.ID, "status": "Bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("move_task with invalid status should set isError, got success: %s", contentText(res))
	}
}

func TestMCPTool_CreateChildTask(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var feature TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Feature"}, &feature)

	var child TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"parent_id": feature.Task.ID,
		"title":     "Child task",
	}, &child)

	if child.Task.ParentID == nil || *child.Task.ParentID != feature.Task.ID {
		t.Errorf("child parent_id = %v, want %d", child.Task.ParentID, feature.Task.ID)
	}
	if child.Task.Kind != "task" {
		t.Errorf("child kind = %q, want %q", child.Task.Kind, "task")
	}
}

func TestMCPTool_CreateSubtask(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var feature TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Feature"}, &feature)
	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"parent_id": feature.Task.ID, "title": "Task",
	}, &task)

	var sub SubtaskOutput
	callTool(ctx, t, cs, "create_subtask", map[string]any{
		"task_id": task.Task.ID,
		"title":   "checklist item",
	}, &sub)
	if sub.Subtask == nil || sub.Subtask.TaskID != task.Task.ID || sub.Subtask.Done {
		t.Fatalf("create_subtask returned %+v, want an item on task %d with done=false", sub.Subtask, task.Task.ID)
	}

	// Toggle it done.
	var toggled SubtaskOutput
	callTool(ctx, t, cs, "toggle_subtask", map[string]any{"id": sub.Subtask.ID, "done": true}, &toggled)
	if !toggled.Subtask.Done {
		t.Errorf("toggle_subtask done = false, want true")
	}

	// Adding a checklist item to a Feature is rejected.
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_subtask",
		Arguments: map[string]any{"task_id": feature.Task.ID, "title": "nope"},
	})
	if err != nil {
		t.Fatalf("create_subtask protocol error: %v", err)
	}
	if !res.IsError {
		t.Error("create_subtask on a Feature: expected isError, got success")
	}
}

func TestMCPTool_AssignTask(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var created TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Assignable"}, &created)

	var assigned TaskOutput
	callTool(ctx, t, cs, "assign_task", map[string]any{
		"id":       created.Task.ID,
		"assignee": "alice",
	}, &assigned)

	if assigned.Task.Assignee != "alice" {
		t.Errorf("assignee = %q, want %q", assigned.Task.Assignee, "alice")
	}
}

func TestMCPTool_DeleteTask_Cascade(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var parent TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Parent"}, &parent)

	var child TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"parent_id": parent.Task.ID,
		"title":     "Child",
	}, &child)
	callTool(ctx, t, cs, "create_subtask", map[string]any{
		"task_id": child.Task.ID,
		"title":   "checklist",
	}, nil)

	callTool(ctx, t, cs, "add_attachment", map[string]any{
		"task_id": parent.Task.ID,
		"type":    "link",
		"url":     "https://example.com",
		"title":   "ref",
	}, nil)

	var del SuccessOutput
	callTool(ctx, t, cs, "delete_task", map[string]any{"id": parent.Task.ID}, &del)
	if !del.Success {
		t.Fatal("delete_task did not report success")
	}

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_task",
		Arguments: map[string]any{"id": parent.Task.ID},
	})
	if err != nil {
		t.Fatalf("CallTool protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("get_task on deleted task should set isError, got: %s", contentText(res))
	}
}

func TestMCPTool_Boards(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	// list_boards starts with just the built-in default board.
	var listed BoardsOutput
	callTool(ctx, t, cs, "list_boards", map[string]any{}, &listed)
	if len(listed.Boards) != 1 || listed.Boards[0].Key != "default" {
		t.Fatalf("initial boards = %+v, want only the default board", listed.Boards)
	}

	// create_board adds a board with its own columns.
	var created BoardMetaOutput
	callTool(ctx, t, cs, "create_board", map[string]any{
		"key":     "team",
		"name":    "Team",
		"columns": []any{"Todo", "Doing", "Done"},
	}, &created)
	if created.Board == nil || created.Board.Key != "team" || len(created.Board.Columns) != 3 {
		t.Fatalf("create_board returned %+v, want a 3-column team board", created.Board)
	}

	// A task created on the board defaults to its first column.
	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"board": "team",
		"title": "scoped task",
	}, &task)
	if string(task.Task.Status) != "Todo" {
		t.Errorf("task status = %q, want %q (board's first column)", task.Task.Status, "Todo")
	}

	// get_board for the team board reflects its columns and the new task.
	var board BoardOutput
	callTool(ctx, t, cs, "get_board", map[string]any{"board": "team"}, &board)
	if board.Board == nil || len(board.Board.Columns) != 3 {
		t.Fatalf("get_board(team) columns = %+v, want 3", board.Board)
	}

	// Moving the task to a column from a different board is rejected.
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "move_task",
		Arguments: map[string]any{"id": task.Task.ID, "status": "Develop"},
	})
	if err != nil {
		t.Fatalf("move_task protocol error: %v", err)
	}
	if !res.IsError {
		t.Error("move_task to a foreign board's column: expected isError, got success")
	}
}

func TestMCPTool_GetBoard(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	for _, tc := range []struct{ title, status string }{
		{"in inbox", "Inbox"},
		{"in plan", "Plan"},
		{"in develop", "Develop"},
	} {
		var out TaskOutput
		callTool(ctx, t, cs, "create_task", map[string]any{
			"title":  tc.title,
			"status": tc.status,
		}, &out)
		callTool(ctx, t, cs, "add_attachment", map[string]any{
			"task_id":  out.Task.ID,
			"type":     "file",
			"filename": "NOTES.md",
			"content":  b64("# notes"),
		}, nil)
	}

	var board BoardOutput
	callTool(ctx, t, cs, "get_board", map[string]any{}, &board)

	if board.Board == nil {
		t.Fatal("get_board returned nil board")
	}
	// 7 workflow columns are always present.
	if len(board.Board.Columns) != 7 {
		t.Fatalf("columns = %d, want 7", len(board.Board.Columns))
	}

	total := 0
	for _, col := range board.Board.Columns {
		for _, task := range col.Tasks {
			total++
			if len(task.Attachments) != 1 {
				t.Errorf("task %d in %s has %d attachments, want 1", task.ID, col.Status, len(task.Attachments))
			}
		}
	}
	if total != 3 {
		t.Errorf("total tasks on board = %d, want 3", total)
	}
}

func TestMCPTool_AddAttachment_File(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Has file"}, &task)

	var att AttachmentOutput
	callTool(ctx, t, cs, "add_attachment", map[string]any{
		"task_id":  task.Task.ID,
		"type":     "file",
		"filename": "DESIGN.md",
		"content":  b64("# Design"),
	}, &att)

	if att.Attachment == nil {
		t.Fatal("add_attachment returned nil attachment")
	}
	if att.Attachment.Type != "file" || att.Attachment.Filename != "DESIGN.md" {
		t.Errorf("attachment = %+v, want file/DESIGN.md", att.Attachment)
	}
}

func TestMCPTool_AddAttachment_UnsupportedType(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Has bad file"}, &task)

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "add_attachment",
		Arguments: map[string]any{
			"task_id":  task.Task.ID,
			"type":     "file",
			"filename": "evil.exe",
			"content":  b64("x"),
		},
	})
	if err != nil {
		t.Fatalf("CallTool protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("add_attachment with unsupported type should set isError, got success: %s", contentText(res))
	}
}

func TestMCPTool_Attributes(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Has attrs"}, &task)

	var attr AttributeOutput
	callTool(ctx, t, cs, "set_attribute", map[string]any{
		"task_id": task.Task.ID, "key": "priority", "value": "high",
	}, &attr)
	if attr.Attribute == nil || attr.Attribute.Key != "priority" || attr.Attribute.Value != "high" {
		t.Fatalf("set_attribute = %+v, want priority=high", attr.Attribute)
	}

	// Upsert: same key replaces the value.
	callTool(ctx, t, cs, "set_attribute", map[string]any{
		"task_id": task.Task.ID, "key": "priority", "value": "low",
	}, &attr)

	var got TaskOutput
	callTool(ctx, t, cs, "get_task", map[string]any{"id": task.Task.ID}, &got)
	if len(got.Task.Attributes) != 1 || got.Task.Attributes[0].Value != "low" {
		t.Fatalf("attributes = %+v, want single priority=low", got.Task.Attributes)
	}
	if len(got.Task.Attachments) != 0 {
		t.Errorf("attachments = %d, want 0 (attributes are separate)", len(got.Task.Attachments))
	}

	// Delete by key.
	callTool(ctx, t, cs, "delete_attribute", map[string]any{
		"task_id": task.Task.ID, "key": "priority",
	}, nil)
	// Decode into a fresh value: the empty attributes field is omitted from the
	// response (omitempty), so reusing the prior struct would keep stale data.
	var afterDelete TaskOutput
	callTool(ctx, t, cs, "get_task", map[string]any{"id": task.Task.ID}, &afterDelete)
	if len(afterDelete.Task.Attributes) != 0 {
		t.Errorf("attributes after delete = %d, want 0", len(afterDelete.Task.Attributes))
	}
}

func TestMCPTool_AddAttachment_Link(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Has link"}, &task)

	var att AttachmentOutput
	callTool(ctx, t, cs, "add_attachment", map[string]any{
		"task_id": task.Task.ID,
		"type":    "link",
		"url":     "https://claude.ai/session/abc",
		"title":   "Agent Session",
	}, &att)

	if att.Attachment.Type != "link" || att.Attachment.URL != "https://claude.ai/session/abc" {
		t.Errorf("attachment = %+v, want link/url", att.Attachment)
	}
}

func TestMCPTool_AddAttachment_ChildTask(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var parent TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Parent"}, &parent)

	var child TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{
		"parent_id": parent.Task.ID,
		"title":     "Child",
	}, &child)

	// Attachments are valid on any card, including a child Task.
	var att AttachmentOutput
	callTool(ctx, t, cs, "add_attachment", map[string]any{
		"task_id": child.Task.ID,
		"type":    "link",
		"url":     "https://example.com",
	}, &att)
	if att.Attachment == nil || att.Attachment.TaskID != child.Task.ID {
		t.Fatalf("add_attachment on child task = %+v, want an attachment on task %d", att.Attachment, child.Task.ID)
	}
}

func TestMCPTool_ShowTaskProgress_Feature(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var feature TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Epic"}, &feature)

	// Two children: one moved to Done, one left in the first column.
	var c1, c2 TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"parent_id": feature.Task.ID, "title": "c1"}, &c1)
	callTool(ctx, t, cs, "create_task", map[string]any{"parent_id": feature.Task.ID, "title": "c2"}, &c2)
	callTool(ctx, t, cs, "move_task", map[string]any{"id": c1.Task.ID, "status": "Done"}, nil)

	var out TaskProgressOutput
	res := callTool(ctx, t, cs, "show_task_progress", map[string]any{"id": feature.Task.ID}, &out)

	if out.Progress == nil {
		t.Fatal("show_task_progress returned nil progress")
	}
	if out.Progress.Kind != "feature" {
		t.Errorf("kind = %q, want feature", out.Progress.Kind)
	}
	if out.Progress.TotalCount != 2 || out.Progress.DoneCount != 1 {
		t.Errorf("counts = %d/%d, want 1/2 done", out.Progress.DoneCount, out.Progress.TotalCount)
	}
	if out.Progress.Board.DoneColumn != "Done" {
		t.Errorf("done_column = %q, want Done", out.Progress.Board.DoneColumn)
	}
	// The text content is the required fallback (a human summary, not the JSON).
	if txt := contentText(res); txt == "" {
		t.Error("show_task_progress returned no text fallback")
	}

	// refresh_task_progress (app-only) returns the same structured shape.
	var refreshed TaskProgressOutput
	callTool(ctx, t, cs, "refresh_task_progress", map[string]any{"id": feature.Task.ID}, &refreshed)
	if refreshed.Progress == nil || refreshed.Progress.TaskID != feature.Task.ID {
		t.Fatalf("refresh_task_progress = %+v, want progress for task %d", refreshed.Progress, feature.Task.ID)
	}
}

// TestMCPTool_TaskProgressApp_Metadata verifies the MCP App wiring: the tools
// advertise the ui:// resourceUri (and the refresh tool is app-only), and the
// ui:// resource serves the View HTML with the MCP App MIME type.
func TestMCPTool_TaskProgressApp_Metadata(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	tools, err := cs.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcpsdk.Tool{}
	for _, tl := range tools.Tools {
		byName[tl.Name] = tl
	}

	show := byName["show_task_progress"]
	if show == nil {
		t.Fatal("show_task_progress not registered")
	}
	if uri := uiResourceURI(show.Meta); uri != "ui://kanban/task-progress" {
		t.Errorf("show_task_progress resourceUri = %q, want ui://kanban/task-progress", uri)
	}

	refresh := byName["refresh_task_progress"]
	if refresh == nil {
		t.Fatal("refresh_task_progress not registered")
	}
	if !hasAppOnlyVisibility(refresh.Meta) {
		t.Errorf("refresh_task_progress should be app-only (visibility:[\"app\"]), meta = %+v", refresh.Meta)
	}

	resources, err := cs.ListResources(ctx, &mcpsdk.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range resources.Resources {
		if r.URI == "ui://kanban/task-progress" {
			found = true
			if r.MIMEType != "text/html;profile=mcp-app" {
				t.Errorf("resource MIME = %q, want text/html;profile=mcp-app", r.MIMEType)
			}
		}
	}
	if !found {
		t.Fatal("ui://kanban/task-progress resource not registered")
	}

	read, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ui://kanban/task-progress"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) == 0 || read.Contents[0].MIMEType != "text/html;profile=mcp-app" {
		t.Fatalf("read contents = %+v, want one text/html;profile=mcp-app block", read.Contents)
	}
	if !strings.Contains(read.Contents[0].Text, "ui/initialize") {
		t.Error("View HTML does not contain the MCP Apps handshake (ui/initialize)")
	}
}

// uiResourceURI extracts _meta.ui.resourceUri, matching how hosts detect an App.
func uiResourceURI(meta map[string]any) string {
	ui, _ := meta["ui"].(map[string]any)
	if ui == nil {
		return ""
	}
	uri, _ := ui["resourceUri"].(string)
	return uri
}

// hasAppOnlyVisibility reports whether _meta.ui.visibility is exactly app-only.
func hasAppOnlyVisibility(meta map[string]any) bool {
	ui, _ := meta["ui"].(map[string]any)
	if ui == nil {
		return false
	}
	vis, ok := ui["visibility"].([]any)
	if !ok || len(vis) == 0 {
		return false
	}
	for _, v := range vis {
		if s, _ := v.(string); s != "app" {
			return false
		}
	}
	return true
}

func TestMCPTool_DeleteAttachment(t *testing.T) {
	ctx := context.Background()
	cs := newTestClient(ctx, t)

	var task TaskOutput
	callTool(ctx, t, cs, "create_task", map[string]any{"title": "Has attachment"}, &task)

	var att AttachmentOutput
	callTool(ctx, t, cs, "add_attachment", map[string]any{
		"task_id": task.Task.ID,
		"type":    "link",
		"url":     "https://example.com",
	}, &att)

	var del SuccessOutput
	callTool(ctx, t, cs, "delete_attachment", map[string]any{"id": att.Attachment.ID}, &del)
	if !del.Success {
		t.Fatal("delete_attachment did not report success")
	}
}
