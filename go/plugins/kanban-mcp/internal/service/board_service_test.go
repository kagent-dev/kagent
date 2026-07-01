package service

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
)

func TestDefaultBoard_Seeded(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	board, err := svc.GetBoardMeta(ctx, db.DefaultBoardKey)
	if err != nil {
		t.Fatalf("GetBoardMeta(default) error = %v", err)
	}
	if len(board.Columns) != len(db.DefaultColumns) {
		t.Errorf("default board columns = %v, want %v", board.Columns, db.DefaultColumns)
	}
	if board.Columns[0] != string(db.StatusInbox) {
		t.Errorf("first column = %q, want %q", board.Columns[0], db.StatusInbox)
	}
}

func TestCreateBoard_AndScopedTask(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	board, err := svc.CreateBoard(ctx, CreateBoardRequest{
		Key:     "team",
		Name:    "Team",
		Columns: []string{"Todo", "Doing", "Done"},
	})
	if err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}
	if board.Scope != db.BoardScopeGeneral {
		t.Errorf("scope = %q, want %q (default)", board.Scope, db.BoardScopeGeneral)
	}

	// A task created on the board defaults to that board's first column.
	task, err := svc.CreateTask(ctx, "team", CreateTaskRequest{Title: "first"})
	if err != nil {
		t.Fatalf("CreateTask(team) error = %v", err)
	}
	if string(task.Status) != "Todo" {
		t.Errorf("default status = %q, want %q (board's first column)", task.Status, "Todo")
	}
	if task.BoardID != board.ID {
		t.Errorf("task.BoardID = %d, want %d", task.BoardID, board.ID)
	}
}

func TestSubtaskTemplate_AutoAddedToTasks(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	board, err := svc.CreateBoard(ctx, CreateBoardRequest{
		Key:      "worker",
		Columns:  []string{"Todo", "Doing", "Done"},
		Subtasks: []string{"Implement", "Open PR", "Monitor CI"},
	})
	if err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}
	if len(board.Subtasks) != 3 {
		t.Fatalf("board.Subtasks = %v, want 3 template items", board.Subtasks)
	}

	// A standalone Task created on the board auto-gets the template subtasks.
	task, err := svc.CreateTask(ctx, "worker", CreateTaskRequest{Title: "do work", Kind: db.KindTask})
	if err != nil {
		t.Fatalf("CreateTask(task) error = %v", err)
	}
	if len(task.Subtasks) != 3 {
		t.Errorf("task.Subtasks = %d, want 3 from template", len(task.Subtasks))
	}
	fetched, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(fetched.Subtasks) != 3 || fetched.Subtasks[0].Title != "Implement" {
		t.Errorf("persisted subtasks = %+v, want 3 template items in order", fetched.Subtasks)
	}

	// A Feature is a container and must not receive checklist subtasks.
	feature, err := svc.CreateTask(ctx, "worker", CreateTaskRequest{Title: "epic", Kind: db.KindFeature})
	if err != nil {
		t.Fatalf("CreateTask(feature) error = %v", err)
	}
	if len(feature.Subtasks) != 0 {
		t.Errorf("feature.Subtasks = %d, want 0 (features have no checklist)", len(feature.Subtasks))
	}

	// A child Task under the Feature also inherits the board's template.
	child, err := svc.CreateTask(ctx, "worker", CreateTaskRequest{Title: "child", ParentID: &feature.ID})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	if len(child.Subtasks) != 3 {
		t.Errorf("child.Subtasks = %d, want 3 from template", len(child.Subtasks))
	}
}

func TestCreateBoard_Validation(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	if _, err := svc.CreateBoard(ctx, CreateBoardRequest{Key: "", Columns: []string{"A"}}); err == nil {
		t.Error("CreateBoard() with empty key: expected error, got nil")
	}
	if _, err := svc.CreateBoard(ctx, CreateBoardRequest{Key: "x", Columns: nil}); err == nil {
		t.Error("CreateBoard() with no columns: expected error, got nil")
	}
	if _, err := svc.CreateBoard(ctx, CreateBoardRequest{Key: "x", Columns: []string{"A"}, Scope: "bogus"}); err == nil {
		t.Error("CreateBoard() with invalid scope: expected error, got nil")
	}
}

func TestMoveTask_RestrictedToBoardColumns(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	if _, err := svc.CreateBoard(ctx, CreateBoardRequest{Key: "team", Columns: []string{"Todo", "Doing", "Done"}}); err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}
	task, err := svc.CreateTask(ctx, "team", CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// Valid move within the board's columns.
	if _, err := svc.MoveTask(ctx, task.ID, db.TaskStatus("Doing")); err != nil {
		t.Fatalf("MoveTask(Doing) error = %v", err)
	}

	// A column from a *different* board (the default board's "Develop") is rejected.
	if _, err := svc.MoveTask(ctx, task.ID, db.StatusDevelop); err == nil {
		t.Error("MoveTask() to a foreign board's column: expected error, got nil")
	}

	// Persisted status is unchanged by the rejected move.
	fetched, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if string(fetched.Status) != "Doing" {
		t.Errorf("status = %q, want %q (rejected move must not persist)", fetched.Status, "Doing")
	}
}

func TestUpsertBoard_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	first, err := svc.UpsertBoard(ctx, CreateBoardRequest{Key: "team", Name: "Team", Columns: []string{"Todo", "Done"}})
	if err != nil {
		t.Fatalf("UpsertBoard() error = %v", err)
	}

	// Re-upserting the same key updates name/columns rather than creating a new row.
	second, err := svc.UpsertBoard(ctx, CreateBoardRequest{Key: "team", Name: "Team v2", Columns: []string{"Backlog", "Todo", "Done"}})
	if err != nil {
		t.Fatalf("UpsertBoard() second error = %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("upsert created a new board (id %d -> %d), want same id", first.ID, second.ID)
	}
	if second.Name != "Team v2" || len(second.Columns) != 3 {
		t.Errorf("upsert did not update fields: %+v", second)
	}

	boards, err := svc.ListBoards(ctx)
	if err != nil {
		t.Fatalf("ListBoards() error = %v", err)
	}
	// default + team = 2.
	if len(boards) != 2 {
		t.Errorf("ListBoards() = %d boards, want 2 (default + team)", len(boards))
	}
}

func TestBoardScopedListing(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	if _, err := svc.CreateBoard(ctx, CreateBoardRequest{Key: "team", Columns: []string{"Todo", "Done"}}); err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}
	if _, err := svc.CreateTask(ctx, "team", CreateTaskRequest{Title: "team task"}); err != nil {
		t.Fatalf("CreateTask(team) error = %v", err)
	}
	if _, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "default task"}); err != nil {
		t.Fatalf("CreateTask(default) error = %v", err)
	}

	teamTasks, err := svc.ListTasks(ctx, "team", TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks(team) error = %v", err)
	}
	if len(teamTasks) != 1 || teamTasks[0].Title != "team task" {
		t.Errorf("team board tasks = %+v, want exactly the team task", teamTasks)
	}

	defaultTasks, err := svc.ListTasks(ctx, "", TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks(default) error = %v", err)
	}
	if len(defaultTasks) != 1 || defaultTasks[0].Title != "default task" {
		t.Errorf("default board tasks = %+v, want exactly the default task", defaultTasks)
	}
}
