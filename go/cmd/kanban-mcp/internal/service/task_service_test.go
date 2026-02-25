package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/service"
	"gorm.io/gorm"
)

// mockBroadcaster records Broadcast calls.
type mockBroadcaster struct {
	calls int
}

func (m *mockBroadcaster) Broadcast(_ interface{}) {
	m.calls++
}

// openTestDB opens a fresh in-memory SQLite DB and auto-migrates the Task model.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	if err := gormDB.AutoMigrate(&db.Task{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return gormDB
}

func TestCreateTask_Defaults(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	task, err := svc.CreateTask(context.Background(), service.CreateTaskRequest{Title: "No Status"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != db.StatusInbox {
		t.Errorf("Status = %q, want %q", task.Status, db.StatusInbox)
	}
}

func TestCreateTask_WithStatus(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	task, err := svc.CreateTask(context.Background(), service.CreateTaskRequest{
		Title:  "Design Task",
		Status: db.StatusDesign,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != db.StatusDesign {
		t.Errorf("Status = %q, want %q", task.Status, db.StatusDesign)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	_, err := svc.GetTask(context.Background(), 9999)
	if err == nil {
		t.Fatal("GetTask() expected error for non-existent task, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("GetTask() error = %v, want wrapped gorm.ErrRecordNotFound", err)
	}
}

func TestMoveTask_Valid(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Move me"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	moved, err := svc.MoveTask(ctx, task.ID, db.StatusDevelop)
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Status != db.StatusDevelop {
		t.Errorf("Status = %q, want %q", moved.Status, db.StatusDevelop)
	}
}

func TestMoveTask_InvalidStatus(t *testing.T) {
	b := &mockBroadcaster{}
	svc := service.NewTaskService(openTestDB(t), b)
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Move me"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	callsBefore := b.calls

	_, err = svc.MoveTask(ctx, task.ID, db.TaskStatus("INVALID"))
	if err == nil {
		t.Fatal("MoveTask() expected error for invalid status, got nil")
	}
	if b.calls != callsBefore {
		t.Error("Broadcast must not be called on invalid status")
	}
}

func TestListTasks_Filter(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Inbox 1"})
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Inbox 2"})
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Design 1", Status: db.StatusDesign})

	status := db.StatusInbox
	tasks, err := svc.ListTasks(ctx, service.TaskFilter{Status: &status})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("ListTasks(Inbox) = %d tasks, want 2", len(tasks))
	}
}

func TestDeleteTask_Simple(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Delete me"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := svc.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	_, err = svc.GetTask(ctx, task.ID)
	if err == nil {
		t.Fatal("GetTask() expected error after deletion, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("GetTask() error = %v, want wrapped gorm.ErrRecordNotFound", err)
	}
}

func TestBroadcast_CalledOnMutation(t *testing.T) {
	b := &mockBroadcaster{}
	svc := service.NewTaskService(openTestDB(t), b)
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Broadcast test"})
	if b.calls != 1 {
		t.Errorf("after CreateTask: Broadcast calls = %d, want 1", b.calls)
	}

	title := "Updated"
	svc.UpdateTask(ctx, task.ID, service.UpdateTaskRequest{Title: &title})
	if b.calls != 2 {
		t.Errorf("after UpdateTask: Broadcast calls = %d, want 2", b.calls)
	}

	svc.MoveTask(ctx, task.ID, db.StatusDesign)
	if b.calls != 3 {
		t.Errorf("after MoveTask: Broadcast calls = %d, want 3", b.calls)
	}

	svc.DeleteTask(ctx, task.ID)
	if b.calls != 4 {
		t.Errorf("after DeleteTask: Broadcast calls = %d, want 4", b.calls)
	}
}

func TestAssignTask(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Assign me"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// Assign to alice
	assigned, err := svc.AssignTask(ctx, task.ID, "alice")
	if err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}
	if assigned.Assignee != "alice" {
		t.Errorf("Assignee = %q, want %q", assigned.Assignee, "alice")
	}

	// Reassign to bob
	reassigned, err := svc.AssignTask(ctx, task.ID, "bob")
	if err != nil {
		t.Fatalf("AssignTask() reassign error = %v", err)
	}
	if reassigned.Assignee != "bob" {
		t.Errorf("Assignee = %q, want %q", reassigned.Assignee, "bob")
	}

	// Clear assignment
	cleared, err := svc.AssignTask(ctx, task.ID, "")
	if err != nil {
		t.Fatalf("AssignTask() clear error = %v", err)
	}
	if cleared.Assignee != "" {
		t.Errorf("Assignee = %q, want empty string", cleared.Assignee)
	}
}

func TestListTasks_AssigneeFilter(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task1, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Alice task 1"})
	task2, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Alice task 2"})
	task3, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Bob task"})
	svc.AssignTask(ctx, task1.ID, "alice")
	svc.AssignTask(ctx, task2.ID, "alice")
	svc.AssignTask(ctx, task3.ID, "bob")

	alice := "alice"
	tasks, err := svc.ListTasks(ctx, service.TaskFilter{Assignee: &alice})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("ListTasks(alice) = %d tasks, want 2", len(tasks))
	}
}

func TestCreateSubtask_Valid(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	parent, err := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	sub, err := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Child"})
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}
	if sub.ParentID == nil || *sub.ParentID != parent.ID {
		t.Errorf("ParentID = %v, want %d", sub.ParentID, parent.ID)
	}
}

func TestCreateSubtask_ParentNotFound(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	_, err := svc.CreateSubtask(context.Background(), 9999, service.CreateTaskRequest{Title: "Orphan"})
	if err == nil {
		t.Fatal("CreateSubtask() expected error for non-existent parent, got nil")
	}
}

func TestCreateSubtask_NestedRejection(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	child, _ := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Child"})

	_, err := svc.CreateSubtask(ctx, child.ID, service.CreateTaskRequest{Title: "Grandchild"})
	if err == nil {
		t.Fatal("CreateSubtask() expected error for nested subtask, got nil")
	}
	if err.Error() != "subtasks cannot have subtasks" {
		t.Errorf("error = %q, want %q", err.Error(), "subtasks cannot have subtasks")
	}
}

func TestDeleteTask_Cascade(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	sub1, _ := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub 1"})
	sub2, _ := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub 2"})

	if err := svc.DeleteTask(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	for _, id := range []uint{parent.ID, sub1.ID, sub2.ID} {
		_, err := svc.GetTask(ctx, id)
		if err == nil {
			t.Errorf("GetTask(%d) expected error after cascade delete, got nil", id)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("GetTask(%d) error = %v, want wrapped gorm.ErrRecordNotFound", id, err)
		}
	}
}

func TestGetTask_WithSubtasks(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub 1"})
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub 2"})

	fetched, err := svc.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(fetched.Subtasks) != 2 {
		t.Errorf("Subtasks count = %d, want 2", len(fetched.Subtasks))
	}
}
