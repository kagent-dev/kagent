package service

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
)

// b64 base64-encodes a string for use as file attachment content (file content
// is stored base64-encoded).
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// dbCountAttachments counts attachment rows for a task directly, bypassing the
// service, to verify persistence and cascade behaviour.
func dbCountAttachments(ctx context.Context, t *testing.T, pool *pgxpool.Pool, taskID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM kanban.attachment WHERE task_id = $1", taskID).Scan(&n); err != nil {
		t.Fatalf("counting attachments for task %d: %v", taskID, err)
	}
	return n
}

func TestAddAttachment_File(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "with file"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	got, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "DESIGN.md",
		Content:  b64("# Design\n\nOverview"),
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if got.Type != db.AttachmentTypeFile {
		t.Errorf("type = %q, want %q", got.Type, db.AttachmentTypeFile)
	}
	if got.Filename != "DESIGN.md" {
		t.Errorf("filename = %q, want %q", got.Filename, "DESIGN.md")
	}
	if got.Content != b64("# Design\n\nOverview") {
		t.Errorf("content = %q, want base64 design content", got.Content)
	}
	if got.TaskID != task.ID {
		t.Errorf("task_id = %d, want %d", got.TaskID, task.ID)
	}

	// Verify persisted, not just echoed.
	fetched, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(fetched.Attachments) != 1 || fetched.Attachments[0].Filename != "DESIGN.md" {
		t.Errorf("persisted attachments = %v, want one DESIGN.md", fetched.Attachments)
	}
}

func TestAddAttachment_Link(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "with link"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	got, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type:  db.AttachmentTypeLink,
		URL:   "https://claude.ai/session/abc",
		Title: "Agent Session",
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if got.Type != db.AttachmentTypeLink {
		t.Errorf("type = %q, want %q", got.Type, db.AttachmentTypeLink)
	}
	if got.URL != "https://claude.ai/session/abc" {
		t.Errorf("url = %q, want session url", got.URL)
	}
	if got.Title != "Agent Session" {
		t.Errorf("title = %q, want %q", got.Title, "Agent Session")
	}
}

func TestAddAttachment_ChildTaskAllowed(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	// Both Features and child Tasks are full cards and may carry attachments.
	feature, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "feature"})
	if err != nil {
		t.Fatalf("CreateTask(feature) error = %v", err)
	}
	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "task", ParentID: &feature.ID})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}

	got, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type:     db.AttachmentTypeFile,
		Filename: "x.md",
		Content:  b64("content"),
	})
	if err != nil {
		t.Fatalf("AddAttachment() on child task error = %v, want success", err)
	}
	if got.TaskID != task.ID {
		t.Errorf("attachment task_id = %d, want %d", got.TaskID, task.ID)
	}
}

func TestAddAttachment_TaskNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	_, err := svc.AddAttachment(ctx, 999999, CreateAttachmentRequest{
		Type: db.AttachmentTypeLink,
		URL:  "https://example.com",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for missing task, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("error = %v, want wrapped pgx.ErrNoRows", err)
	}
}

func TestAddAttachment_InvalidType(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_, err = svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{Type: db.AttachmentType("invalid")})
	if err == nil {
		t.Fatal("AddAttachment() expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "file") || !strings.Contains(err.Error(), "link") {
		t.Errorf("error = %q, want it to list valid types file and link", err.Error())
	}
}

func TestAddAttachment_FileMissingFields(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	tests := []struct {
		name string
		req  CreateAttachmentRequest
	}{
		{
			name: "empty filename",
			req:  CreateAttachmentRequest{Type: db.AttachmentTypeFile, Content: "data"},
		},
		{
			name: "empty content",
			req:  CreateAttachmentRequest{Type: db.AttachmentTypeFile, Filename: "x.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.AddAttachment(ctx, task.ID, tt.req); err == nil {
				t.Fatalf("AddAttachment() expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestAddAttachment_LinkMissingURL(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{Type: db.AttachmentTypeLink, Title: "no url"}); err == nil {
		t.Fatal("AddAttachment() expected error for missing url, got nil")
	}
}

func TestDeleteAttachment_Valid(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	att, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com",
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}

	if err := svc.DeleteAttachment(ctx, att.ID); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}
	if n := dbCountAttachments(ctx, t, pool, task.ID); n != 0 {
		t.Errorf("attachment count after delete = %d, want 0", n)
	}
}

func TestDeleteAttachment_NotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	err := svc.DeleteAttachment(ctx, 999999)
	if err == nil {
		t.Fatal("DeleteAttachment() expected error for missing attachment, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("error = %v, want wrapped pgx.ErrNoRows", err)
	}
}

func TestDeleteTask_CascadeWithAttachments(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newTestService(ctx, t)

	parent, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "parent"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.AddAttachment(ctx, parent.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "a.md", Content: b64("a"),
	}); err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if _, err := svc.AddAttachment(ctx, parent.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com",
	}); err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	child, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "child", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}

	if err := svc.DeleteTask(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if _, err := svc.GetTask(ctx, parent.ID); !IsNotFound(err) {
		t.Errorf("GetTask(parent) error = %v, want not-found", err)
	}
	if _, err := svc.GetTask(ctx, child.ID); !IsNotFound(err) {
		t.Errorf("GetTask(child) error = %v, want not-found", err)
	}
	if n := dbCountAttachments(ctx, t, pool, parent.ID); n != 0 {
		t.Errorf("attachment count after cascade delete = %d, want 0", n)
	}
}

func TestGetTask_WithAttachments(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "one.md", Content: b64("1"),
	}); err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if _, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com", Title: "link",
	}); err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}

	got, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(got.Attachments))
	}
	if got.Attachments[0].Filename != "one.md" {
		t.Errorf("first attachment filename = %q, want %q", got.Attachments[0].Filename, "one.md")
	}
	if got.Attachments[1].Type != db.AttachmentTypeLink {
		t.Errorf("second attachment type = %q, want %q", got.Attachments[1].Type, db.AttachmentTypeLink)
	}
}

func TestAddAttachment_UnsupportedFileType(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_, err = svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "malware.exe", Content: b64("x"),
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for unsupported file type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported file type") {
		t.Errorf("error = %q, want unsupported-file-type message", err.Error())
	}
}

func TestAddAttachment_FileNotBase64(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_, err = svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeFile, Filename: "notes.txt", Content: "%%% not base64 %%%",
	})
	if err == nil {
		t.Fatal("AddAttachment() expected error for non-base64 content, got nil")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error = %q, want base64 message", err.Error())
	}
}

func TestSetAttribute_Upsert(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	attr, err := svc.SetAttribute(ctx, task.ID, "priority", "high")
	if err != nil {
		t.Fatalf("SetAttribute() error = %v", err)
	}
	if attr.Key != "priority" || attr.Value != "high" {
		t.Errorf("attr = %+v, want priority=high", attr)
	}

	// Setting the same key again replaces the value (upsert).
	if _, err := svc.SetAttribute(ctx, task.ID, "priority", "low"); err != nil {
		t.Fatalf("SetAttribute(upsert) error = %v", err)
	}

	got, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(got.Attributes) != 1 {
		t.Fatalf("attributes = %d, want 1 (upsert, not duplicate)", len(got.Attributes))
	}
	if got.Attributes[0].Value != "low" {
		t.Errorf("attribute value = %q, want %q", got.Attributes[0].Value, "low")
	}
	// Attributes must not be reported as file/link attachments.
	if len(got.Attachments) != 0 {
		t.Errorf("attachments = %d, want 0 (attributes are separate)", len(got.Attachments))
	}
}

func TestSetAttribute_EmptyKey(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.SetAttribute(ctx, task.ID, "  ", "v"); err == nil {
		t.Fatal("SetAttribute() expected error for empty key, got nil")
	}
}

func TestDeleteAttribute(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.SetAttribute(ctx, task.ID, "team", "platform"); err != nil {
		t.Fatalf("SetAttribute() error = %v", err)
	}

	if err := svc.DeleteAttribute(ctx, task.ID, "team"); err != nil {
		t.Fatalf("DeleteAttribute() error = %v", err)
	}
	got, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("attributes after delete = %d, want 0", len(got.Attributes))
	}

	// Deleting a missing key is a not-found.
	if err := svc.DeleteAttribute(ctx, task.ID, "team"); !IsNotFound(err) {
		t.Errorf("DeleteAttribute(missing) error = %v, want not-found", err)
	}
}

func TestBroadcast_CalledOnAttachmentMutation(t *testing.T) {
	ctx := context.Background()
	svc, b, _ := newTestService(ctx, t)

	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "host"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	before := b.calls()
	att, err := svc.AddAttachment(ctx, task.ID, CreateAttachmentRequest{
		Type: db.AttachmentTypeLink, URL: "https://example.com",
	})
	if err != nil {
		t.Fatalf("AddAttachment() error = %v", err)
	}
	if got := b.calls() - before; got < 1 {
		t.Errorf("add: Broadcast called %d times, want >= 1", got)
	}

	before = b.calls()
	if err := svc.DeleteAttachment(ctx, att.ID); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}
	if got := b.calls() - before; got < 1 {
		t.Errorf("delete: Broadcast called %d times, want >= 1", got)
	}
}
