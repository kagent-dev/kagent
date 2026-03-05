package mcp_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	kanbanmcp "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// nopBroadcaster is a no-op Broadcaster for testing.
type nopBroadcaster struct{}

func (n *nopBroadcaster) Broadcast(_ interface{}) {}

// setupTest creates an in-memory SQLite db and returns a connected MCP client session.
func setupTest(t *testing.T) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: dbPath,
	}
	mgr, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	svc := service.NewTaskService(mgr.DB(), &nopBroadcaster{})
	server := kanbanmcp.NewServer(svc)

	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()

	_, err = server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}

	return cs, func() { cs.Close() }
}

// callTool is a helper to call an MCP tool and return the text content.
func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]interface{}) *mcpsdk.CallToolResult {
	t.Helper()
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

// extractText returns the text from the first TextContent item of a result.
func extractText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is not *TextContent")
	}
	return tc.Text
}

func TestMCPTool_CreateTask(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	result := callTool(t, cs, "create_task", map[string]interface{}{
		"title":  "Fix bug",
		"status": "Design",
	})

	if result.IsError {
		t.Fatalf("create_task returned error: %s", extractText(t, result))
	}

	var task db.Task
	if err := json.Unmarshal([]byte(extractText(t, result)), &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}

	if task.Title != "Fix bug" {
		t.Errorf("title = %q, want %q", task.Title, "Fix bug")
	}
	if task.Status != db.StatusDesign {
		t.Errorf("status = %q, want %q", task.Status, db.StatusDesign)
	}
	if task.ID == 0 {
		t.Error("task.ID should be non-zero")
	}
}

func TestMCPTool_MoveTask_Invalid(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	// Create a task first
	createResult := callTool(t, cs, "create_task", map[string]interface{}{
		"title": "Some task",
	})
	if createResult.IsError {
		t.Fatalf("create_task failed: %s", extractText(t, createResult))
	}

	var task db.Task
	if err := json.Unmarshal([]byte(extractText(t, createResult)), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Move to invalid status
	moveResult := callTool(t, cs, "move_task", map[string]interface{}{
		"id":     task.ID,
		"status": "INVALID",
	})

	if !moveResult.IsError {
		t.Error("move_task with invalid status should return isError:true")
	}
}

func TestMCPTool_CreateSubtask(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	// Create parent task
	parentResult := callTool(t, cs, "create_task", map[string]interface{}{
		"title": "Parent task",
	})
	if parentResult.IsError {
		t.Fatalf("create_task failed: %s", extractText(t, parentResult))
	}

	var parent db.Task
	if err := json.Unmarshal([]byte(extractText(t, parentResult)), &parent); err != nil {
		t.Fatalf("unmarshal parent: %v", err)
	}

	// Create subtask
	subResult := callTool(t, cs, "create_subtask", map[string]interface{}{
		"parent_id": parent.ID,
		"title":     "Subtask one",
	})
	if subResult.IsError {
		t.Fatalf("create_subtask failed: %s", extractText(t, subResult))
	}

	var subtask db.Task
	if err := json.Unmarshal([]byte(extractText(t, subResult)), &subtask); err != nil {
		t.Fatalf("unmarshal subtask: %v", err)
	}

	if subtask.ParentID == nil {
		t.Fatal("subtask.ParentID should not be nil")
	}
	if *subtask.ParentID != parent.ID {
		t.Errorf("subtask.ParentID = %d, want %d", *subtask.ParentID, parent.ID)
	}
}

func TestMCPTool_AssignTask(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	// Create task
	createResult := callTool(t, cs, "create_task", map[string]interface{}{
		"title": "Assign me",
	})
	if createResult.IsError {
		t.Fatalf("create_task failed")
	}

	var task db.Task
	if err := json.Unmarshal([]byte(extractText(t, createResult)), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Assign
	assignResult := callTool(t, cs, "assign_task", map[string]interface{}{
		"id":       task.ID,
		"assignee": "alice",
	})
	if assignResult.IsError {
		t.Fatalf("assign_task failed: %s", extractText(t, assignResult))
	}

	var updated db.Task
	if err := json.Unmarshal([]byte(extractText(t, assignResult)), &updated); err != nil {
		t.Fatalf("unmarshal updated: %v", err)
	}

	if updated.Assignee != "alice" {
		t.Errorf("assignee = %q, want %q", updated.Assignee, "alice")
	}
}

func TestMCPTool_DeleteTask_Cascade(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	// Create parent
	parentResult := callTool(t, cs, "create_task", map[string]interface{}{
		"title": "Parent",
	})
	if parentResult.IsError {
		t.Fatalf("create parent failed")
	}
	var parent db.Task
	if err := json.Unmarshal([]byte(extractText(t, parentResult)), &parent); err != nil {
		t.Fatalf("unmarshal parent: %v", err)
	}

	// Create subtask
	subResult := callTool(t, cs, "create_subtask", map[string]interface{}{
		"parent_id": parent.ID,
		"title":     "Child",
	})
	if subResult.IsError {
		t.Fatalf("create subtask failed: %s", extractText(t, subResult))
	}

	// Delete parent
	deleteResult := callTool(t, cs, "delete_task", map[string]interface{}{
		"id": parent.ID,
	})
	if deleteResult.IsError {
		t.Fatalf("delete_task failed: %s", extractText(t, deleteResult))
	}

	// Verify parent is gone
	getResult := callTool(t, cs, "get_task", map[string]interface{}{
		"id": parent.ID,
	})
	if !getResult.IsError {
		t.Error("get_task after delete should return error")
	}
}

func TestMCPTool_GetBoard(t *testing.T) {
	cs, cleanup := setupTest(t)
	defer cleanup()

	// Create tasks in different statuses
	for _, args := range []map[string]interface{}{
		{"title": "Task A", "status": "Inbox"},
		{"title": "Task B", "status": "Design"},
		{"title": "Task C", "status": "Develop"},
	} {
		r := callTool(t, cs, "create_task", args)
		if r.IsError {
			t.Fatalf("create_task failed: %s", extractText(t, r))
		}
	}

	boardResult := callTool(t, cs, "get_board", map[string]interface{}{})
	if boardResult.IsError {
		t.Fatalf("get_board failed: %s", extractText(t, boardResult))
	}

	var board kanbanmcp.Board
	if err := json.Unmarshal([]byte(extractText(t, boardResult)), &board); err != nil {
		t.Fatalf("unmarshal board: %v", err)
	}

	if len(board.Columns) != len(db.StatusWorkflow) {
		t.Errorf("board has %d columns, want %d", len(board.Columns), len(db.StatusWorkflow))
	}

	// Verify tasks are in the right columns
	found := map[string]int{}
	for _, col := range board.Columns {
		found[col.Status] = len(col.Tasks)
	}

	if found["Inbox"] != 1 {
		t.Errorf("Inbox column has %d tasks, want 1", found["Inbox"])
	}
	if found["Design"] != 1 {
		t.Errorf("Design column has %d tasks, want 1", found["Design"])
	}
	if found["Develop"] != 1 {
		t.Errorf("Develop column has %d tasks, want 1", found["Develop"])
	}
}
