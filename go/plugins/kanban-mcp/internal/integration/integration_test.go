// Package integration holds an end-to-end Postgres integration test for the
// kanban-mcp server (Step 11). It exercises the full TaskService workflow —
// create, update, move, assign, subtask, attachment, attribute and cascade delete —
// against a real Postgres instance.
//
// The test uses a testcontainers Postgres by default and is skipped when Docker
// is unavailable. Set KANBAN_TEST_POSTGRES_URL to run against an existing
// Postgres instead (the test isolates itself by re-running migrations, which is
// a no-op if the kanban schema already exists).
package integration

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	kdb "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

// postgresURL returns a Postgres connection string, preferring the
// KANBAN_TEST_POSTGRES_URL env var and otherwise starting a testcontainer.
// Tests skip when neither is available.
func postgresURL(ctx context.Context, t *testing.T) string {
	t.Helper()

	if url := os.Getenv("KANBAN_TEST_POSTGRES_URL"); url != "" {
		return url
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available and KANBAN_TEST_POSTGRES_URL unset; skipping integration test")
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
	return connStr
}

// TestPostgres_Integration drives a full CRUD + subtask + assign + attachment +
// cascade workflow against a real Postgres, verifying the migration runner,
// connection helper and service layer all cooperate.
func TestPostgres_Integration(t *testing.T) {
	ctx := context.Background()
	url := postgresURL(ctx, t)

	// Resolve + migrate + connect: the same sequence main.go performs at startup.
	resolved, err := kdb.ResolveURL(url, "")
	if err != nil {
		t.Fatalf("ResolveURL() error = %v", err)
	}
	if err := migrations.RunUp(resolved); err != nil {
		t.Fatalf("RunUp() error = %v", err)
	}

	pool, err := kdb.Connect(ctx, resolved)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	svc := service.NewTaskService(dbgen.New(pool), pool, service.NopBroadcaster{})

	// 1. Create a top-level task.
	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{
		Title:       "Ship Step 11",
		Description: "Dockerfile + Helm + integration test",
		Labels:      []string{"infra", "release"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != kdb.StatusInbox {
		t.Errorf("default status = %q, want %q", task.Status, kdb.StatusInbox)
	}
	if len(task.Labels) != 2 {
		t.Errorf("labels = %v, want 2 entries", task.Labels)
	}

	// 2. Update fields.
	newTitle := "Ship Step 11 (final)"
	uin := true
	updated, err := svc.UpdateTask(ctx, task.ID, service.UpdateTaskRequest{
		Title:           &newTitle,
		UserInputNeeded: &uin,
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Title != newTitle || !updated.UserInputNeeded {
		t.Errorf("UpdateTask() = %+v, want title=%q user_input_needed=true", updated, newTitle)
	}

	// 3. Move through the workflow.
	moved, err := svc.MoveTask(ctx, task.ID, kdb.StatusDevelop)
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Status != kdb.StatusDevelop {
		t.Errorf("status after move = %q, want %q", moved.Status, kdb.StatusDevelop)
	}

	// 3b. Invalid move is rejected.
	if _, err := svc.MoveTask(ctx, task.ID, kdb.TaskStatus("Nope")); err == nil {
		t.Error("MoveTask() with invalid status: expected error, got nil")
	}

	// 4. Assign and re-assign.
	if _, err := svc.AssignTask(ctx, task.ID, "alice"); err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}
	assignee := "alice"
	listed, err := svc.ListTasks(ctx, "", service.TaskFilter{Assignee: &assignee})
	if err != nil {
		t.Fatalf("ListTasks(assignee) error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != task.ID {
		t.Errorf("ListTasks(assignee=alice) = %d tasks, want exactly task %d", len(listed), task.ID)
	}

	// 5. Feature → Task → Subtask hierarchy. `task` is a Feature; create a child
	//    Task under it, then a checklist subtask on the child Task.
	child, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "write Dockerfile", ParentID: &task.ID})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	if child.ParentID == nil || *child.ParentID != task.ID || child.Kind != kdb.KindTask {
		t.Errorf("child task = %+v, want a Task with parent %d", child, task.ID)
	}
	// A Task cannot parent another Task (one level of Feature→Task only).
	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "nested", ParentID: &child.ID}); err == nil {
		t.Error("CreateTask() nested under a Task: expected error, got nil")
	}
	sub, err := svc.CreateSubtask(ctx, child.ID, "compile binary")
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}
	if sub.TaskID != child.ID || sub.Done {
		t.Errorf("subtask = %+v, want task_id=%d done=false", sub, child.ID)
	}
	// Checklist subtasks can only be added to Tasks, not Features.
	if _, err := svc.CreateSubtask(ctx, task.ID, "nope"); err == nil {
		t.Error("CreateSubtask() on a Feature: expected error, got nil")
	}

	// 6. Attachments on a card (file on the Feature + link on the child Task);
	//    file content is stored base64-encoded.
	fileAtt, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type:     kdb.AttachmentTypeFile,
		Filename: "DESIGN.md",
		Content:  base64.StdEncoding.EncodeToString([]byte("# Design\n\nKanban server.")),
	})
	if err != nil {
		t.Fatalf("AddAttachment(file) error = %v", err)
	}
	linkAtt, err := svc.AddAttachment(ctx, child.ID, service.CreateAttachmentRequest{
		Type:  kdb.AttachmentTypeLink,
		URL:   "https://claude.ai/session/abc",
		Title: "Agent Session",
	})
	if err != nil {
		t.Fatalf("AddAttachment(link) error = %v", err)
	}

	// 6b. Key/value attributes on the Feature (same table, type=attribute).
	if _, err := svc.SetAttribute(ctx, task.ID, "priority", "high"); err != nil {
		t.Fatalf("SetAttribute() error = %v", err)
	}
	// Upsert replaces the value rather than adding a duplicate.
	if _, err := svc.SetAttribute(ctx, task.ID, "priority", "critical"); err != nil {
		t.Fatalf("SetAttribute(upsert) error = %v", err)
	}

	// 7. GetTask assembles children/checklist/attachments/attributes.
	full, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask(feature) error = %v", err)
	}
	if len(full.Children) != 1 {
		t.Errorf("GetTask(feature).Children = %d, want 1", len(full.Children))
	}
	// Attributes are reported separately from file/link attachments.
	if len(full.Attachments) != 1 {
		t.Errorf("GetTask(feature).Attachments = %d, want 1", len(full.Attachments))
	}
	if len(full.Attributes) != 1 || full.Attributes[0].Key != "priority" || full.Attributes[0].Value != "critical" {
		t.Errorf("GetTask(feature).Attributes = %+v, want single priority=critical", full.Attributes)
	}
	fullChild, err := svc.GetTask(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetTask(child) error = %v", err)
	}
	if len(fullChild.Subtasks) != 1 {
		t.Errorf("GetTask(child).Subtasks = %d, want 1", len(fullChild.Subtasks))
	}

	// 8. Board groups all cards (Feature + child Task) by status.
	board, err := svc.GetBoard(ctx, "")
	if err != nil {
		t.Fatalf("GetBoard() error = %v", err)
	}
	if len(board.Columns) != len(kdb.StatusWorkflow) {
		t.Errorf("board columns = %d, want %d", len(board.Columns), len(kdb.StatusWorkflow))
	}
	cards := 0
	for _, col := range board.Columns {
		cards += len(col.Tasks)
	}
	if cards != 2 {
		t.Errorf("cards on board = %d, want 2 (feature + child task)", cards)
	}

	// 9. Delete one attachment explicitly.
	if err := svc.DeleteAttachment(ctx, linkAtt.ID); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}

	// 10. Cascade delete: removing the Feature removes the child Task, its
	//     checklist subtask, and remaining attachments.
	if err := svc.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	if _, err := svc.GetTask(ctx, task.ID); err == nil {
		t.Error("GetTask() after delete: expected not-found error, got nil")
	}

	// Verify directly in Postgres that the cascade emptied every table.
	assertCount(ctx, t, pool, "SELECT count(*) FROM kanban.task", 0)
	assertCount(ctx, t, pool, "SELECT count(*) FROM kanban.attachment", 0)
	assertCount(ctx, t, pool, "SELECT count(*) FROM kanban.subtask", 0)
	_ = fileAtt // file attachment row removed by cascade; kept for assertion clarity
}

// TestPostgres_Boards drives the dynamic-boards workflow against a real Postgres:
// the seeded default board, a runtime-created board with its own columns,
// board-scoped task listing, and the rule that a task may only move between its
// own board's columns.
func TestPostgres_Boards(t *testing.T) {
	ctx := context.Background()
	url := postgresURL(ctx, t)

	resolved, err := kdb.ResolveURL(url, "")
	if err != nil {
		t.Fatalf("ResolveURL() error = %v", err)
	}
	if err := migrations.RunUp(resolved); err != nil {
		t.Fatalf("RunUp() error = %v", err)
	}
	pool, err := kdb.Connect(ctx, resolved)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM kanban.task")
		_, _ = pool.Exec(ctx, "DELETE FROM kanban.board WHERE key <> 'default'")
	})

	svc := service.NewTaskService(dbgen.New(pool), pool, service.NopBroadcaster{})

	// The default board is seeded by migration 000002 with the 7 workflow columns.
	def, err := svc.GetBoardMeta(ctx, kdb.DefaultBoardKey)
	if err != nil {
		t.Fatalf("GetBoardMeta(default) error = %v", err)
	}
	if len(def.Columns) != len(kdb.DefaultColumns) {
		t.Errorf("default board columns = %v, want %v", def.Columns, kdb.DefaultColumns)
	}

	// Create a board with its own columns.
	board, err := svc.CreateBoard(ctx, service.CreateBoardRequest{
		Key:     "team",
		Name:    "Team",
		Columns: []string{"Todo", "Doing", "Done"},
	})
	if err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}

	// A task on the board defaults to that board's first column.
	task, err := svc.CreateTask(ctx, "team", service.CreateTaskRequest{Title: "scoped"})
	if err != nil {
		t.Fatalf("CreateTask(team) error = %v", err)
	}
	if string(task.Status) != "Todo" || task.BoardID != board.ID {
		t.Errorf("task = {status:%q board:%d}, want {Todo %d}", task.Status, task.BoardID, board.ID)
	}

	// Move within the board is allowed; move to a foreign board's column is rejected.
	if _, err := svc.MoveTask(ctx, task.ID, kdb.TaskStatus("Doing")); err != nil {
		t.Fatalf("MoveTask(Doing) error = %v", err)
	}
	if _, err := svc.MoveTask(ctx, task.ID, kdb.StatusDevelop); err == nil {
		t.Error("MoveTask() to default board's 'Develop' on a team task: expected error, got nil")
	}

	// Board-scoped listing isolates tasks per board.
	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "on default"}); err != nil {
		t.Fatalf("CreateTask(default) error = %v", err)
	}
	teamTasks, err := svc.ListTasks(ctx, "team", service.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks(team) error = %v", err)
	}
	if len(teamTasks) != 1 || teamTasks[0].Title != "scoped" {
		t.Errorf("team tasks = %+v, want only the scoped task", teamTasks)
	}

	// UpsertBoard is idempotent: same key updates instead of inserting.
	again, err := svc.UpsertBoard(ctx, service.CreateBoardRequest{
		Key:     "team",
		Name:    "Team v2",
		Columns: []string{"Backlog", "Todo", "Doing", "Done"},
	})
	if err != nil {
		t.Fatalf("UpsertBoard() error = %v", err)
	}
	if again.ID != board.ID || again.Name != "Team v2" || len(again.Columns) != 4 {
		t.Errorf("upsert = %+v, want same id with updated name/columns", again)
	}
}

func assertCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, query string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Errorf("%q = %d, want %d", query, got, want)
	}
}
