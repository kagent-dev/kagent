package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// startPostgres starts a Postgres container, runs the kanban migrations, and
// returns a connection string. Tests skip when Docker is not available. This is
// a thin local copy of go/core/internal/dbtest (which cannot be imported across
// the internal/ boundary), mirroring the other package test helpers.
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

// newTestServer starts Postgres, wires the REST handlers onto a mux, and returns
// an httptest.Server plus the live TaskService for direct verification.
func newTestServer(ctx context.Context, t *testing.T) (*httptest.Server, *service.TaskService) {
	t.Helper()
	url := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var svc *service.TaskService
	hub := sse.NewHub(func(board string) any {
		state, berr := svc.GetBoard(context.Background(), board)
		if berr != nil {
			return &service.BoardState{Columns: []service.Column{}}
		}
		return state
	})
	svc = service.NewTaskService(dbgen.New(pool), pool, hub)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", hub.ServeSSE)
	mux.HandleFunc("/api/tasks", TasksHandler(svc))
	mux.HandleFunc("/api/tasks/", TaskHandler(svc))
	mux.HandleFunc("/api/subtasks/", SubtaskHandler(svc))
	mux.HandleFunc("/api/attachments/", AttachmentHandler(svc))
	mux.HandleFunc("/api/board", BoardHandler(svc))
	mux.HandleFunc("/api/boards", BoardsHandler(svc))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, svc
}

// doReq performs an HTTP request with an optional JSON body and returns the
// status code and raw body.
func doReq(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp.StatusCode, out
}

func decodeTask(t *testing.T, b []byte) *service.Task {
	t.Helper()
	var task service.Task
	if err := json.Unmarshal(b, &task); err != nil {
		t.Fatalf("decoding task: %v (body=%s)", err, b)
	}
	return &task
}

func TestREST_CreateTask(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks", map[string]any{
		"title":  "Ship it",
		"status": "Inbox",
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", status, body)
	}
	task := decodeTask(t, body)
	if task.ID == 0 || task.Title != "Ship it" || task.Status != "Inbox" {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestREST_GetTask(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	created, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "T"})
	if err != nil {
		t.Fatalf("seeding task: %v", err)
	}

	tests := []struct {
		name       string
		id         string
		wantStatus int
	}{
		{name: "found", id: itoa(created.ID), wantStatus: http.StatusOK},
		{name: "missing", id: "99999", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := doReq(t, http.MethodGet, ts.URL+"/api/tasks/"+tt.id, nil)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}

func TestREST_UpdateTask(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	created, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "T"})
	if err != nil {
		t.Fatalf("seeding task: %v", err)
	}

	status, body := doReq(t, http.MethodPut, ts.URL+"/api/tasks/"+itoa(created.ID),
		map[string]any{"status": "Plan"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	if got := decodeTask(t, body).Status; got != "Plan" {
		t.Fatalf("status = %q, want Plan", got)
	}
}

func TestREST_UpdateTask_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	created, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "T"})
	if err != nil {
		t.Fatalf("seeding task: %v", err)
	}

	status, _ := doReq(t, http.MethodPut, ts.URL+"/api/tasks/"+itoa(created.ID),
		map[string]any{"status": "Bogus"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

func TestREST_ListTasks_Filter(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "a", Status: "Inbox"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "b", Status: "Plan"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, body := doReq(t, http.MethodGet, ts.URL+"/api/tasks?status=Inbox", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var tasks []*service.Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "a" {
		t.Fatalf("unexpected filtered tasks: %+v", tasks)
	}
}

func TestREST_ChildTask(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	feature, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "feature"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a child Task via POST /api/tasks with parent_id.
	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks",
		map[string]any{"title": "child", "parent_id": feature.ID})
	if status != http.StatusCreated {
		t.Fatalf("create child task status = %d, want 201 (body=%s)", status, body)
	}
	child := decodeTask(t, body)
	if child.ParentID == nil || *child.ParentID != feature.ID {
		t.Fatalf("child parent_id = %v, want %d", child.ParentID, feature.ID)
	}
	if child.Kind != "task" {
		t.Errorf("child kind = %q, want %q", child.Kind, "task")
	}
}

func TestREST_Subtasks(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	feature, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "feature"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "task", ParentID: &feature.ID})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// Create a checklist subtask.
	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/subtasks",
		map[string]any{"title": "item"})
	if status != http.StatusCreated {
		t.Fatalf("create subtask status = %d, want 201 (body=%s)", status, body)
	}
	var sub service.Subtask
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatalf("decode subtask: %v (body=%s)", err, body)
	}
	if sub.TaskID != task.ID || sub.Done {
		t.Fatalf("subtask = %+v, want task_id=%d done=false", sub, task.ID)
	}

	// Toggle it done via PUT /api/subtasks/{id}.
	status, body = doReq(t, http.MethodPut, ts.URL+"/api/subtasks/"+itoa(sub.ID),
		map[string]any{"done": true})
	if status != http.StatusOK {
		t.Fatalf("toggle subtask status = %d, want 200 (body=%s)", status, body)
	}

	// List subtasks.
	status, body = doReq(t, http.MethodGet, ts.URL+"/api/tasks/"+itoa(task.ID)+"/subtasks", nil)
	if status != http.StatusOK {
		t.Fatalf("list subtasks status = %d, want 200", status)
	}
	var subs []*service.Subtask
	if err := json.Unmarshal(body, &subs); err != nil {
		t.Fatalf("decode subtasks: %v", err)
	}
	if len(subs) != 1 || !subs[0].Done {
		t.Fatalf("got subtasks %+v, want 1 done item", subs)
	}

	// Delete it.
	status, _ = doReq(t, http.MethodDelete, ts.URL+"/api/subtasks/"+itoa(sub.ID), nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete subtask status = %d, want 204", status)
	}
}

func TestREST_DeleteTask_Cascade(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	parent, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "parent"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "child", ParentID: &parent.ID}); err != nil {
		t.Fatalf("seed child task: %v", err)
	}

	status, _ := doReq(t, http.MethodDelete, ts.URL+"/api/tasks/"+itoa(parent.ID), nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", status)
	}

	if _, err := svc.GetTask(ctx, parent.ID); !service.IsNotFound(err) {
		t.Fatalf("parent should be gone, got err=%v", err)
	}
}

func TestREST_Board(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	if _, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t", Status: "Inbox"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, body := doReq(t, http.MethodGet, ts.URL+"/api/board", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var board service.BoardState
	if err := json.Unmarshal(body, &board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	if len(board.Columns) == 0 {
		t.Fatal("board has no columns")
	}
}

// b64 base64-encodes a string for use as file attachment content.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestREST_AddAttachment_File(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attachments",
		map[string]any{"type": "file", "filename": "DESIGN.md", "content": b64("# Design")})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", status, body)
	}
	var att service.Attachment
	if err := json.Unmarshal(body, &att); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if att.Filename != "DESIGN.md" || att.Type != "file" {
		t.Fatalf("unexpected attachment: %+v", att)
	}
}

func TestREST_AddAttachment_Link(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attachments",
		map[string]any{"type": "link", "url": "https://example.com", "title": "Session"})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", status, body)
	}
}

func TestREST_AddAttachment_ChildTask(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	parent, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "parent"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	child, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "child", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("seed child task: %v", err)
	}

	// Attachments are valid on a child Task (it is a full card).
	status, _ := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(child.ID)+"/attachments",
		map[string]any{"type": "file", "filename": "a.md", "content": b64("x")})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}
}

func TestREST_AddAttachment_UnsupportedType(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, _ := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attachments",
		map[string]any{"type": "file", "filename": "evil.exe", "content": b64("x")})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unsupported file type", status)
	}
}

func TestREST_Attributes(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Upsert an attribute.
	status, body := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attributes",
		map[string]any{"key": "priority", "value": "high"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var attr service.Attribute
	if err := json.Unmarshal(body, &attr); err != nil {
		t.Fatalf("decode attribute: %v", err)
	}
	if attr.Key != "priority" || attr.Value != "high" {
		t.Fatalf("attribute = %+v, want priority=high", attr)
	}

	// Upsert replaces the value; the task then has exactly one attribute.
	if status, _ := doReq(t, http.MethodPost, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attributes",
		map[string]any{"key": "priority", "value": "low"}); status != http.StatusOK {
		t.Fatalf("upsert status = %d, want 200", status)
	}
	fetched, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(fetched.Attributes) != 1 || fetched.Attributes[0].Value != "low" {
		t.Fatalf("attributes = %+v, want single priority=low", fetched.Attributes)
	}

	// Delete by key.
	if status, _ := doReq(t, http.MethodDelete, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attributes?key=priority", nil); status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", status)
	}
	// Deleting a missing key is a 404.
	if status, _ := doReq(t, http.MethodDelete, ts.URL+"/api/tasks/"+itoa(task.ID)+"/attributes?key=priority", nil); status != http.StatusNotFound {
		t.Fatalf("delete-missing status = %d, want 404", status)
	}
}

func TestREST_DeleteAttachment(t *testing.T) {
	ctx := context.Background()
	ts, svc := newTestServer(ctx, t)

	task, err := svc.CreateTask(ctx, "", service.CreateTaskRequest{Title: "t"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	att, err := svc.AddAttachment(ctx, task.ID, service.CreateAttachmentRequest{
		Type: "file", Filename: "a.md", Content: b64("x"),
	})
	if err != nil {
		t.Fatalf("seed attachment: %v", err)
	}

	status, _ := doReq(t, http.MethodDelete, ts.URL+"/api/attachments/"+itoa(att.ID), nil)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
}

func TestREST_DeleteAttachment_NotFound(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	status, _ := doReq(t, http.MethodDelete, ts.URL+"/api/attachments/99999", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

// TestREST_SSE_AfterMutation connects to the SSE stream and verifies a board
// update is pushed after a REST mutation.
func TestREST_SSE_AfterMutation(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ts.URL+"/events", nil)
	if err != nil {
		t.Fatalf("building SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connecting to /events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	// Drain the initial snapshot event.
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}

	// Trigger a mutation via REST.
	go func() {
		_, _ = doReq(t, http.MethodPost, ts.URL+"/api/tasks", map[string]any{"title": "live"})
	}()

	done := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event:") || strings.HasPrefix(scanner.Text(), "data:") {
				done <- true
				return
			}
		}
		done <- false
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("did not receive an SSE event after mutation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SSE event after mutation")
	}
}

// itoa formats an int64 as a decimal path segment.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
