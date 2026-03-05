package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kanbanapi "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// newTestAPI creates a fully-wired test HTTP server backed by an in-memory SQLite DB.
func newTestAPI(t *testing.T) (*httptest.Server, *service.TaskService, *sse.Hub) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := &config.Config{DBType: config.DBTypeSQLite, DBPath: dbPath}
	mgr, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	hub := sse.NewHub()
	svc := service.NewTaskService(mgr.DB(), hub)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", kanbanapi.TasksHandler(svc))
	mux.HandleFunc("/api/tasks/", kanbanapi.TaskHandler(svc))
	mux.HandleFunc("/api/board", kanbanapi.BoardHandler(svc))
	mux.HandleFunc("/events", hub.ServeSSE)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, svc, hub
}

func TestREST_CreateTask(t *testing.T) {
	ts, _, _ := newTestAPI(t)

	body := `{"title":"Fix bug","status":"Inbox"}`
	resp, err := http.Post(ts.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var task db.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if task.Title != "Fix bug" {
		t.Errorf("expected title 'Fix bug', got %q", task.Title)
	}
	if task.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestREST_GetTask(t *testing.T) {
	ts, svc, _ := newTestAPI(t)

	created, _ := svc.CreateTask(context.Background(), service.CreateTaskRequest{Title: "Test"})

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks/%d", ts.URL, created.ID))
	if err != nil {
		t.Fatalf("GET /api/tasks/%d: %v", created.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var task db.Task
	json.NewDecoder(resp.Body).Decode(&task) //nolint:errcheck
	if task.ID != created.ID {
		t.Errorf("expected ID %d, got %d", created.ID, task.ID)
	}

	// 404 for missing task
	resp404, _ := http.Get(ts.URL + "/api/tasks/99999")
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp404.StatusCode)
	}
}

func TestREST_UpdateTask(t *testing.T) {
	ts, svc, _ := newTestAPI(t)

	created, _ := svc.CreateTask(context.Background(), service.CreateTaskRequest{Title: "Orig"})

	body := `{"status":"Design"}`
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/tasks/%d", ts.URL, created.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/tasks/%d: %v", created.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var task db.Task
	json.NewDecoder(resp.Body).Decode(&task) //nolint:errcheck
	if task.Status != db.StatusDesign {
		t.Errorf("expected Design, got %q", task.Status)
	}
}

func TestREST_ListTasks_Filter(t *testing.T) {
	ts, svc, _ := newTestAPI(t)
	ctx := context.Background()

	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task1", Status: db.StatusInbox})  //nolint:errcheck
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Task2", Status: db.StatusDesign}) //nolint:errcheck

	resp, err := http.Get(ts.URL + "/api/tasks?status=Inbox")
	if err != nil {
		t.Fatalf("GET /api/tasks?status=Inbox: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var tasks []*db.Task
	json.NewDecoder(resp.Body).Decode(&tasks) //nolint:errcheck
	if len(tasks) != 1 {
		t.Errorf("expected 1 Inbox task, got %d", len(tasks))
	}
	if len(tasks) > 0 && tasks[0].Title != "Task1" {
		t.Errorf("expected Task1, got %q", tasks[0].Title)
	}
}

func TestREST_Subtasks_Create(t *testing.T) {
	ts, svc, _ := newTestAPI(t)

	parent, _ := svc.CreateTask(context.Background(), service.CreateTaskRequest{Title: "Parent"})

	body := `{"title":"Subtask","status":"Inbox"}`
	resp, err := http.Post(fmt.Sprintf("%s/api/tasks/%d/subtasks", ts.URL, parent.ID), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subtasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var subtask db.Task
	json.NewDecoder(resp.Body).Decode(&subtask) //nolint:errcheck
	if subtask.ParentID == nil || *subtask.ParentID != parent.ID {
		t.Errorf("expected ParentID=%d, got %v", parent.ID, subtask.ParentID)
	}
}

func TestREST_Subtasks_List(t *testing.T) {
	ts, svc, _ := newTestAPI(t)
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub1"}) //nolint:errcheck
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub2"}) //nolint:errcheck

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks/%d/subtasks", ts.URL, parent.ID))
	if err != nil {
		t.Fatalf("GET subtasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var tasks []*db.Task
	json.NewDecoder(resp.Body).Decode(&tasks) //nolint:errcheck
	if len(tasks) != 2 {
		t.Errorf("expected 2 subtasks, got %d", len(tasks))
	}
}

func TestREST_DeleteTask_Cascade(t *testing.T) {
	ts, svc, _ := newTestAPI(t)
	ctx := context.Background()

	parent, _ := svc.CreateTask(ctx, service.CreateTaskRequest{Title: "Parent"})
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub1"}) //nolint:errcheck
	svc.CreateSubtask(ctx, parent.ID, service.CreateTaskRequest{Title: "Sub2"}) //nolint:errcheck

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/tasks/%d", ts.URL, parent.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/tasks/%d: %v", parent.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// Verify parent is gone
	if _, err := svc.GetTask(ctx, parent.ID); err == nil {
		t.Error("expected error getting deleted task, got nil")
	}

	// Verify subtasks are gone
	pid := parent.ID
	subs, _ := svc.ListTasks(ctx, service.TaskFilter{ParentID: &pid})
	if len(subs) != 0 {
		t.Errorf("expected 0 subtasks after cascade delete, got %d", len(subs))
	}
}

func TestREST_Board(t *testing.T) {
	ts, svc, _ := newTestAPI(t)
	ctx := context.Background()

	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "T1", Status: db.StatusInbox})  //nolint:errcheck
	svc.CreateTask(ctx, service.CreateTaskRequest{Title: "T2", Status: db.StatusDesign}) //nolint:errcheck

	resp, err := http.Get(ts.URL + "/api/board")
	if err != nil {
		t.Fatalf("GET /api/board: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var board kanbanapi.Board
	json.NewDecoder(resp.Body).Decode(&board) //nolint:errcheck
	if len(board.Columns) != len(db.StatusWorkflow) {
		t.Errorf("expected %d columns, got %d", len(db.StatusWorkflow), len(board.Columns))
	}
	inboxCount := 0
	for _, col := range board.Columns {
		if col.Status == "Inbox" {
			inboxCount = len(col.Tasks)
		}
	}
	if inboxCount != 1 {
		t.Errorf("expected 1 task in Inbox, got %d", inboxCount)
	}
}

func TestREST_SSE_AfterMutation(t *testing.T) {
	ts, _, _ := newTestAPI(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	events := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				events <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Consume the initial snapshot
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial snapshot")
	}

	// POST a new task to trigger a board_update broadcast
	body := `{"title":"SSE Test","status":"Inbox"}`
	postResp, err := http.Post(ts.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/tasks: %v", err)
	}
	postResp.Body.Close()

	// Wait for board_update event
	select {
	case data := <-events:
		if !strings.Contains(data, "board_update") {
			t.Errorf("expected board_update in SSE data, got: %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for SSE board_update event")
	}
}
