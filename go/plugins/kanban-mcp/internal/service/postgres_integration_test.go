package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

// TestPostgres_Integration runs a full CRUD + subtask + assign workflow against
// a real Postgres database. It is skipped unless KANBAN_TEST_POSTGRES_URL is set.
//
// Example:
//
//	KANBAN_TEST_POSTGRES_URL="host=localhost user=postgres password=test dbname=postgres port=5432 sslmode=disable" \
//	  go test ./cmd/kanban-mcp/internal/service/... -run TestPostgres_Integration -v
func TestPostgres_Integration(t *testing.T) {
	pgURL := os.Getenv("KANBAN_TEST_POSTGRES_URL")
	if pgURL == "" {
		t.Skip("KANBAN_TEST_POSTGRES_URL not set; skipping Postgres integration test")
	}

	cfg := &config.Config{
		DBType: config.DBTypePostgres,
		DBURL:  pgURL,
	}

	mgr, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Clean up any leftover rows from a previous run.
	mgr.DB().Exec("DELETE FROM tasks")

	svc := service.NewTaskService(mgr.DB(), &mockBroadcaster{})
	ctx := context.Background()

	// ---- CreateTask ----
	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{
		Title:  "PG task",
		Status: db.StatusDesign,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID == 0 {
		t.Fatal("CreateTask() returned task with ID=0")
	}
	if task.Status != db.StatusDesign {
		t.Errorf("status = %q, want %q", task.Status, db.StatusDesign)
	}

	// ---- GetTask ----
	got, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Title != "PG task" {
		t.Errorf("title = %q, want %q", got.Title, "PG task")
	}

	// ---- MoveTask ----
	moved, err := svc.MoveTask(ctx, task.ID, db.StatusDevelop)
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Status != db.StatusDevelop {
		t.Errorf("moved status = %q, want %q", moved.Status, db.StatusDevelop)
	}

	// ---- AssignTask ----
	assigned, err := svc.AssignTask(ctx, task.ID, "alice")
	if err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}
	if assigned.Assignee != "alice" {
		t.Errorf("assignee = %q, want %q", assigned.Assignee, "alice")
	}

	// ---- CreateSubtask ----
	sub, err := svc.CreateSubtask(ctx, task.ID, service.CreateTaskRequest{
		Title: "PG subtask",
	})
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}
	if sub.ParentID == nil || *sub.ParentID != task.ID {
		t.Errorf("subtask parent_id = %v, want %d", sub.ParentID, task.ID)
	}

	// ---- GetTask includes subtasks ----
	withSubs, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask(with subtasks) error = %v", err)
	}
	if len(withSubs.Subtasks) != 1 {
		t.Errorf("subtask count = %d, want 1", len(withSubs.Subtasks))
	}

	// ---- UpdateTask ----
	newTitle := "PG task updated"
	updated, err := svc.UpdateTask(ctx, task.ID, service.UpdateTaskRequest{
		Title: &newTitle,
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("updated title = %q, want %q", updated.Title, newTitle)
	}

	// ---- ListTasks ----
	tasks, err := svc.ListTasks(ctx, service.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) == 0 {
		t.Error("ListTasks() returned empty slice, expected at least 1 task")
	}

	// ---- DeleteTask cascades subtasks ----
	if err := svc.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	_, err = svc.GetTask(ctx, task.ID)
	if err == nil {
		t.Error("GetTask() after delete expected error, got nil")
	}
	_, err = svc.GetTask(ctx, sub.ID)
	if err == nil {
		t.Error("GetTask(subtask) after cascade delete expected error, got nil")
	}
}
