package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"gorm.io/gorm"
)

// mockBroadcaster records Broadcast calls.
type mockBroadcaster struct {
	calls int
}

func (m *mockBroadcaster) Broadcast(_ interface{}) {
	m.calls++
}

// openTestDB opens a fresh SQLite DB and auto-migrates the Task and Attachment models.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	if err := gormDB.AutoMigrate(&db.Task{}, &db.Attachment{}); err != nil {
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

func TestCreateTask_WithLabels(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	task, err := svc.CreateTask(context.Background(), service.CreateTaskRequest{
		Title:  "Labeled Task",
		Labels: []string{"priority:high", "team:platform"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if len(task.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(task.Labels))
	}
	if task.Labels[0] != "priority:high" || task.Labels[1] != "team:platform" {
		t.Errorf("Labels = %v, want [priority:high, team:platform]", task.Labels)
	}

	// Verify labels persist after re-fetch
	fetched, err := svc.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(fetched.Labels) != 2 {
		t.Errorf("Fetched Labels count = %d, want 2", len(fetched.Labels))
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

func TestListTasks_LabelFilter(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task A", Labels: []string{"priority:high", "team:platform"}})
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task B", Labels: []string{"priority:low"}})
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task C", Labels: []string{"priority:high", "team:infra"}})

	label := "priority:high"
	tasks, err := svc.ListTasks(ctx, service.TaskFilter{Label: &label})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("ListTasks(priority:high) = %d tasks, want 2", len(tasks))
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

// --- Attachment tests ---

func TestAddAttachment_File(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task with file"})
	att, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "DESIGN.md",
		Content:  "# Design\n\nOverview",
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if att.Type != db.AttachmentTypeFile {
		t.Errorf("Type = %q, want %q", att.Type, db.AttachmentTypeFile)
	}
	if att.Filename != "DESIGN.md" {
		t.Errorf("Filename = %q, want %q", att.Filename, "DESIGN.md")
	}
	if att.TaskID != task.ID {
		t.Errorf("TaskID = %d, want %d", att.TaskID, task.ID)
	}
}

func TestAddAttachment_Link(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task with link"})
	att, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:  db.AttachmentTypeLink,
		URL:   "https://example.com",
		Title: "Reference",
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if att.Type != db.AttachmentTypeLink {
		t.Errorf("Type = %q, want %q", att.Type, db.AttachmentTypeLink)
	}
	if att.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", att.URL, "https://example.com")
	}
}

func TestAddAttachment_SubtaskRejected(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	sub, _ := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Child"})

	_, err := svc.AddAttachment(ctx, sub.ID, service.CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "test.md",
		Content:  "content",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for subtask, got nil")
	}
	if err.Error() != "attachments can only be added to top-level tasks" {
		t.Errorf("error = %q, want %q", err.Error(), "attachments can only be added to top-level tasks")
	}
}

func TestAddAttachment_TaskNotFound(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	_, err := svc.AddAttachment(context.Background(), 9999, service.CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "test.md",
		Content:  "content",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for non-existent task, got nil")
	}
}

func TestAddAttachment_InvalidType(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})
	_, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentType("invalid"),
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for invalid type, got nil")
	}
	if err.Error() != "type must be 'file' or 'link'" {
		t.Errorf("error = %q, want %q", err.Error(), "type must be 'file' or 'link'")
	}
}

func TestAddAttachment_FileMissingFields(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})

	// Missing filename
	_, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:    db.AttachmentTypeFile,
		Content: "content",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for missing filename, got nil")
	}

	// Missing content
	_, err = svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "test.md",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for missing content, got nil")
	}
}

func TestAddAttachment_LinkMissingURL(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})
	_, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:  db.AttachmentTypeLink,
		Title: "No URL",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for missing URL, got nil")
	}
}

func TestDeleteAttachment_Valid(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})
	att, _ := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "test.md",
		Content:  "content",
	})

	if err := svc.DeleteAttachment(ctx, att.ID); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}

	// Verify attachment is gone
	fetched, _ := svc.GetTask(ctx, task.ID)
	if len(fetched.Attachments) != 0 {
		t.Errorf("Attachments count = %d after delete, want 0", len(fetched.Attachments))
	}
}

func TestDeleteAttachment_NotFound(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})

	err := svc.DeleteAttachment(context.Background(), 9999)
	if err == nil {
		t.Fatal("DeleteAttachment() expected error for non-existent attachment, got nil")
	}
}

func TestDeleteTask_CascadeWithAttachments(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()
	gormDB := openTestDB(t)
	svc = service.NewTaskService(gormDB, &mockBroadcaster{})

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	svc.AddAttachment(ctx, parent.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "a.md", Content: "a",
	})
	svc.AddAttachment(ctx, parent.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com",
	})
	sub, _ := svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub"})

	if err := svc.DeleteTask(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	// Verify parent and subtask gone
	for _, id := range []uint{parent.ID, sub.ID} {
		_, err := svc.GetTask(ctx, id)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("GetTask(%d) should return not-found after cascade delete", id)
		}
	}

	// Verify attachments gone
	var count int64
	gormDB.Model(&db.Attachment{}).Where("task_id = ?", parent.ID).Count(&count)
	if count != 0 {
		t.Errorf("Attachments count = %d after cascade delete, want 0", count)
	}
}

func TestGetTask_WithAttachments(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task with attachments"})
	svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "a.md", Content: "content",
	})
	svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com", Title: "Link",
	})

	fetched, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(fetched.Attachments) != 2 {
		t.Errorf("Attachments count = %d, want 2", len(fetched.Attachments))
	}
}

func TestBroadcast_CalledOnAttachmentMutation(t *testing.T) {
	b := &mockBroadcaster{}
	svc := service.NewTaskService(openTestDB(t), b)
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})
	callsBefore := b.calls

	att, _ := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "test.md", Content: "content",
	})
	if b.calls != callsBefore+1 {
		t.Errorf("after AddAttachment: Broadcast calls = %d, want %d", b.calls, callsBefore+1)
	}

	svc.DeleteAttachment(ctx, att.ID)
	if b.calls != callsBefore+2 {
		t.Errorf("after DeleteAttachment: Broadcast calls = %d, want %d", b.calls, callsBefore+2)
	}
}

func TestUpdateTask_Labels(t *testing.T) {
	svc := service.NewTaskService(openTestDB(t), &mockBroadcaster{})
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task"})

	labels := []string{"priority:high", "group:platform"}
	updated, err := svc.UpdateTask(ctx, task.ID, service.UpdateTaskRequest{Labels: &labels})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if len(updated.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(updated.Labels))
	}

	// Verify deduplication
	dupeLabels := []string{"a", "A", "b"}
	updated, err = svc.UpdateTask(ctx, task.ID, service.UpdateTaskRequest{Labels: &dupeLabels})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if len(updated.Labels) != 2 {
		t.Errorf("Labels count after dedup = %d, want 2", len(updated.Labels))
	}
}
