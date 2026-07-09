package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
)

// fakeTaskStore is an in-memory TaskStore. Sessions are keyed by (id, userID)
// so cross-user isolation is exercised the same way the real store enforces it.
type fakeTaskStore struct {
	sessions map[string]dbpkg.Session // key: sessionID -> session (carries UserID)
	tasks    map[string][]*a2atype.Task
}

func newFakeStore() *fakeTaskStore {
	return &fakeTaskStore{
		sessions: map[string]dbpkg.Session{},
		tasks:    map[string][]*a2atype.Task{},
	}
}

func (f *fakeTaskStore) addSession(id, userID string) {
	f.sessions[id] = dbpkg.Session{ID: id, UserID: userID}
}

func (f *fakeTaskStore) addTask(sessionID string, task *a2atype.Task) {
	f.tasks[sessionID] = append(f.tasks[sessionID], task)
}

func (f *fakeTaskStore) GetSession(_ context.Context, sessionID, userID string) (*dbpkg.Session, error) {
	s, ok := f.sessions[sessionID]
	if !ok || s.UserID != userID {
		return nil, fmt.Errorf("session %s for user %s: %w", sessionID, userID, dbpkg.ErrNotFound)
	}
	return &s, nil
}

func (f *fakeTaskStore) ListSessions(_ context.Context, userID string) ([]dbpkg.Session, error) {
	var out []dbpkg.Session
	for _, s := range f.sessions {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeTaskStore) ListTasksForSession(_ context.Context, sessionID string) ([]*a2atype.Task, error) {
	return f.tasks[sessionID], nil
}

// fakeSession injects a user principal into the request context.
type fakeSession struct{ user string }

func (f fakeSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: f.user}}
}

func userCtx(user string) context.Context {
	return auth.AuthSessionTo(context.Background(), fakeSession{user: user})
}

func newTask(id, contextID string, state a2atype.TaskState, history, artifacts int) *a2atype.Task {
	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	t := &a2atype.Task{
		ID:        a2atype.TaskID(id),
		ContextID: contextID,
		Status:    a2atype.TaskStatus{State: state, Timestamp: &ts},
	}
	for i := range history {
		t.History = append(t.History, &a2atype.Message{ID: fmt.Sprintf("%s-msg-%d", id, i), Role: a2atype.MessageRoleUser})
	}
	for i := range artifacts {
		t.Artifacts = append(t.Artifacts, &a2atype.Artifact{ID: a2atype.ArtifactID(fmt.Sprintf("%s-art-%d", id, i))})
	}
	return t
}

func storeWith(t *testing.T, user, session string, tasks ...*a2atype.Task) *storeTaskQueryHandler {
	t.Helper()
	store := newFakeStore()
	store.addSession(session, user)
	for _, tk := range tasks {
		store.addTask(session, tk)
	}
	return newStoreTaskQueryHandler(&PassthroughRequestHandler{}, store)
}

func TestListTasks_Pagination(t *testing.T) {
	tasks := []*a2atype.Task{
		newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t2", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t3", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t4", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t5", "s1", a2atype.TaskStateWorking, 0, 0),
	}
	h := storeWith(t, "alice", "s1", tasks...)
	ctx := userCtx("alice")

	var seen []string
	token := ""
	pages := 0
	for {
		resp, err := h.ListTasks(ctx, &a2atype.ListTasksRequest{ContextID: "s1", PageSize: 2, PageToken: token})
		require.NoError(t, err)
		require.Equal(t, 5, resp.TotalSize)
		require.Equal(t, 2, resp.PageSize)
		for _, tk := range resp.Tasks {
			seen = append(seen, string(tk.ID))
		}
		pages++
		if resp.NextPageToken == "" {
			break
		}
		token = resp.NextPageToken
		require.LessOrEqual(t, pages, 5, "pagination did not terminate")
	}
	require.Equal(t, 3, pages)
	require.Equal(t, []string{"t1", "t2", "t3", "t4", "t5"}, seen)
}

func TestListTasks_NextPageTokenAlwaysPresentEmptyOnLastPage(t *testing.T) {
	h := storeWith(t, "alice", "s1",
		newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t2", "s1", a2atype.TaskStateWorking, 0, 0),
	)
	resp, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "s1", PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Tasks, 2)
	require.Equal(t, "", resp.NextPageToken, "nextPageToken must be empty string on the final page")

	// Empty result set still reports an (empty) nextPageToken.
	empty, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "does-not-exist"})
	require.NoError(t, err)
	require.Empty(t, empty.Tasks)
	require.Equal(t, "", empty.NextPageToken)
}

func TestListTasks_IncludeArtifacts(t *testing.T) {
	h := storeWith(t, "alice", "s1", newTask("t1", "s1", a2atype.TaskStateCompleted, 0, 3))
	ctx := userCtx("alice")

	off, err := h.ListTasks(ctx, &a2atype.ListTasksRequest{ContextID: "s1"})
	require.NoError(t, err)
	require.Nil(t, off.Tasks[0].Artifacts, "artifacts must be omitted when includeArtifacts is false (default)")

	on, err := h.ListTasks(ctx, &a2atype.ListTasksRequest{ContextID: "s1", IncludeArtifacts: true})
	require.NoError(t, err)
	require.Len(t, on.Tasks[0].Artifacts, 3)
}

func TestListTasks_StatusFilter(t *testing.T) {
	h := storeWith(t, "alice", "s1",
		newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0),
		newTask("t2", "s1", a2atype.TaskStateInputRequired, 0, 0),
		newTask("t3", "s1", a2atype.TaskStateInputRequired, 0, 0),
	)
	resp, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "s1", Status: a2atype.TaskStateInputRequired})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalSize)
	for _, tk := range resp.Tasks {
		require.Equal(t, a2atype.TaskStateInputRequired, tk.Status.State)
	}
}

func TestListTasks_StatusTimestampAfter(t *testing.T) {
	early := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	old := newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0)
	old.Status.Timestamp = &early
	recent := newTask("t2", "s1", a2atype.TaskStateWorking, 0, 0)
	recent.Status.Timestamp = &late

	h := storeWith(t, "alice", "s1", old, recent)
	cutoff := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	resp, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "s1", StatusTimestampAfter: &cutoff})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalSize)
	require.Equal(t, "t2", string(resp.Tasks[0].ID))
}

func TestListTasks_HistoryLength(t *testing.T) {
	h := storeWith(t, "alice", "s1", newTask("t1", "s1", a2atype.TaskStateWorking, 5, 0))
	n := 2
	resp, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "s1", HistoryLength: &n})
	require.NoError(t, err)
	require.Len(t, resp.Tasks[0].History, 2)
	// The most recent messages are kept.
	require.Equal(t, "t1-msg-3", resp.Tasks[0].History[0].ID)
	require.Equal(t, "t1-msg-4", resp.Tasks[0].History[1].ID)
}

func TestCrossUserIsolation(t *testing.T) {
	store := newFakeStore()
	store.addSession("s1", "alice")
	store.addTask("s1", newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0))
	h := newStoreTaskQueryHandler(&PassthroughRequestHandler{}, store)

	// mallory asks for alice's context id: gets nothing.
	resp, err := h.ListTasks(userCtx("mallory"), &a2atype.ListTasksRequest{ContextID: "s1"})
	require.NoError(t, err)
	require.Empty(t, resp.Tasks)
	require.Equal(t, 0, resp.TotalSize)

	// Unauthenticated context: empty, never a leak.
	respAnon, err := h.ListTasks(context.Background(), &a2atype.ListTasksRequest{ContextID: "s1"})
	require.NoError(t, err)
	require.Empty(t, respAnon.Tasks)
}

func TestListTasks_ShareContextGrantsOwnerSession(t *testing.T) {
	store := newFakeStore()
	store.addSession("s1", "alice")
	store.addTask("s1", newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0))
	h := newStoreTaskQueryHandler(&PassthroughRequestHandler{}, store)

	// A visitor (bob) holding a share token for alice's session s1 lists it.
	ctx := auth.ShareContextTo(userCtx("bob"), &auth.ShareContext{SessionID: "s1", UserID: "alice"})
	resp, err := h.ListTasks(ctx, &a2atype.ListTasksRequest{ContextID: "s1"})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalSize)
	require.Equal(t, "t1", string(resp.Tasks[0].ID))

	// A share token for a different session must not grant s1.
	ctxOther := auth.ShareContextTo(userCtx("bob"), &auth.ShareContext{SessionID: "s2", UserID: "alice"})
	respOther, err := h.ListTasks(ctxOther, &a2atype.ListTasksRequest{ContextID: "s1"})
	require.NoError(t, err)
	require.Empty(t, respOther.Tasks)

	// The share token must not widen an all-sessions query to the owner's tasks.
	respAll, err := h.ListTasks(ctx, &a2atype.ListTasksRequest{})
	require.NoError(t, err)
	require.Empty(t, respAll.Tasks, "share token grants one session, not the owner's whole account")
}

func TestListTasks_AcrossAllUserSessions(t *testing.T) {
	store := newFakeStore()
	store.addSession("s1", "alice")
	store.addSession("s2", "alice")
	store.addSession("s3", "bob")
	store.addTask("s1", newTask("t1", "s1", a2atype.TaskStateWorking, 0, 0))
	store.addTask("s2", newTask("t2", "s2", a2atype.TaskStateWorking, 0, 0))
	store.addTask("s3", newTask("t3", "s3", a2atype.TaskStateWorking, 0, 0))
	h := newStoreTaskQueryHandler(&PassthroughRequestHandler{}, store)

	resp, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalSize)
	got := []string{string(resp.Tasks[0].ID), string(resp.Tasks[1].ID)}
	require.ElementsMatch(t, []string{"t1", "t2"}, got)
}

// failingTaskStore fails every read with a backend error.
type failingTaskStore struct{ err error }

func (f failingTaskStore) GetSession(context.Context, string, string) (*dbpkg.Session, error) {
	return nil, f.err
}

func (f failingTaskStore) ListSessions(context.Context, string) ([]dbpkg.Session, error) {
	return nil, f.err
}

func (f failingTaskStore) ListTasksForSession(context.Context, string) ([]*a2atype.Task, error) {
	return nil, f.err
}

func TestListTasks_BackendFailurePropagates(t *testing.T) {
	backendErr := fmt.Errorf("failed to get session s1: connection refused")
	h := newStoreTaskQueryHandler(&PassthroughRequestHandler{}, failingTaskStore{err: backendErr})

	// A store failure must surface as an error, not an empty task list.
	_, err := h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{ContextID: "s1"})
	require.ErrorContains(t, err, "connection refused")

	_, err = h.ListTasks(userCtx("alice"), &a2atype.ListTasksRequest{})
	require.ErrorContains(t, err, "connection refused")
}

// ── Wire tests: identical filtering, different enum casing ──────────────────

func withUser(next http.Handler, user string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.AuthSessionTo(r.Context(), fakeSession{user: user})))
	})
}

func rpcCall(t *testing.T, h http.Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), "raw: %s", rec.Body.String())
	return out
}

func wireHandlers(user string, tasks ...*a2atype.Task) (v1 http.Handler, v0 http.Handler) {
	store := newFakeStore()
	store.addSession("s1", user)
	for _, tk := range tasks {
		store.addTask("s1", tk)
	}
	h := newStoreTaskQueryHandler(&PassthroughRequestHandler{}, store)
	v1 = withUser(a2asrv.NewJSONRPCHandler(h), user)
	v0 = withUser(newV0TasksListInterceptor(a2av0.NewJSONRPCHandler(h), h), user)
	return v1, v0
}

func TestWire_ListTasksStateCasing(t *testing.T) {
	tasks := []*a2atype.Task{
		newTask("t1", "s1", a2atype.TaskStateInputRequired, 0, 0),
		newTask("t2", "s1", a2atype.TaskStateWorking, 0, 0),
	}
	v1, v0 := wireHandlers("alice", tasks...)

	// v1 wire: uppercase TaskState, method "ListTasks".
	v1resp := rpcCall(t, v1, `{"jsonrpc":"2.0","id":1,"method":"ListTasks","params":{"contextId":"s1","status":"TASK_STATE_INPUT_REQUIRED"}}`)
	v1result := v1resp["result"].(map[string]any)
	v1list := v1result["tasks"].([]any)
	require.Len(t, v1list, 1)
	require.Equal(t, float64(1), v1result["totalSize"])
	require.Contains(t, v1result, "nextPageToken")
	v1state := v1list[0].(map[string]any)["status"].(map[string]any)["state"].(string)
	require.Equal(t, "TASK_STATE_INPUT_REQUIRED", v1state)

	// v0 wire: lowercase TaskState, method "tasks/list".
	v0resp := rpcCall(t, v0, `{"jsonrpc":"2.0","id":1,"method":"tasks/list","params":{"contextId":"s1","status":"input-required"}}`)
	v0result := v0resp["result"].(map[string]any)
	v0list := v0result["tasks"].([]any)
	require.Len(t, v0list, 1, "v0 must filter identically to v1")
	require.Equal(t, float64(1), v0result["totalSize"])
	require.Contains(t, v0result, "nextPageToken")
	v0state := v0list[0].(map[string]any)["status"].(map[string]any)["state"].(string)
	require.Equal(t, "input-required", v0state)
}

func TestWire_V0UnknownMethodDelegates(t *testing.T) {
	_, v0 := wireHandlers("alice")
	// A method the interceptor does not own must fall through to the v0 handler,
	// which reports method-not-found rather than being swallowed here.
	resp := rpcCall(t, v0, `{"jsonrpc":"2.0","id":1,"method":"tasks/frobnicate","params":{}}`)
	require.Contains(t, resp, "error")
}

func TestWire_V0TasksListWithoutStoreIsMethodNotFound(t *testing.T) {
	// Without a task store the v0 interceptor must not be installed: tasks/list
	// keeps the legacy wire's native method-not-found instead of hitting the
	// passthrough (which would surface ErrUnsupportedOperation).
	_, legacy := newTaskQueryHandlers(&PassthroughRequestHandler{}, nil)
	resp := rpcCall(t, withUser(legacy, "alice"), `{"jsonrpc":"2.0","id":1,"method":"tasks/list","params":{}}`)
	require.Contains(t, resp, "error")
	errObj := resp["error"].(map[string]any)
	require.Equal(t, float64(-32601), errObj["code"], "expected JSON-RPC method-not-found")
}
