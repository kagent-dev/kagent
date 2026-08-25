package taskstore

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

func newTestStore(t *testing.T, dbURL, user string) *Local {
	t.Helper()
	store, err := New(dbURL, func(context.Context) (string, error) { return user, nil })
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testTask(id, contextID string, state a2atype.TaskState, timestamp *time.Time) *a2atype.Task {
	return &a2atype.Task{
		ID:        a2atype.TaskID(id),
		ContextID: contextID,
		Status:    a2atype.TaskStatus{State: state, Timestamp: timestamp},
		History: []*a2atype.Message{
			{ID: id + "-message-1", Role: a2atype.MessageRoleUser},
			{ID: id + "-message-2", Role: a2atype.MessageRoleAgent},
			{ID: id + "-message-3", Role: a2atype.MessageRoleUser},
		},
		Artifacts: []*a2atype.Artifact{{ID: a2atype.ArtifactID(id + "-artifact")}},
	}
}

func TestLocalPersistsInputRequiredTask(t *testing.T) {
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	auth := func(context.Context) (string, error) { return "alice", nil }
	store, err := New(dbURL, auth)
	if err != nil {
		t.Fatal(err)
	}
	task := a2atype.NewSubmittedTask(&a2atype.Message{ID: "message", ContextID: "context"}, nil)
	task.Status.State = a2atype.TaskStateInputRequired
	version, err := store.Create(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := New(dbURL, auth)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Task.Status.State != a2atype.TaskStateInputRequired || stored.Version != version {
		t.Fatalf("reopened task = (%s, %d), want (%s, %d)", stored.Task.Status.State, stored.Version, a2atype.TaskStateInputRequired, version)
	}
	otherUser, err := New(dbURL, func(context.Context) (string, error) { return "bob", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherUser.Get(context.Background(), task.ID); err != a2atype.ErrTaskNotFound {
		t.Fatalf("other user error = %v, want %v", err, a2atype.ErrTaskNotFound)
	}

	stored.Task.Status.State = a2atype.TaskStateCompleted
	if _, err := reopened.Update(context.Background(), &a2ataskstore.UpdateRequest{Task: stored.Task, PrevVersion: stored.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Update(context.Background(), &a2ataskstore.UpdateRequest{Task: stored.Task, PrevVersion: stored.Version}); err != a2ataskstore.ErrConcurrentModification {
		t.Fatalf("stale update error = %v, want %v", err, a2ataskstore.ErrConcurrentModification)
	}
}

func TestLocalCreateGet(t *testing.T) {
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	store := newTestStore(t, dbURL, "alice")
	task := testTask("task", "context", a2atype.TaskStateWorking, nil)

	if _, err := store.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(t.Context(), task); !errors.Is(err, a2ataskstore.ErrTaskAlreadyExists) {
		t.Fatalf("duplicate create error = %v, want %v", err, a2ataskstore.ErrTaskAlreadyExists)
	}

	task.ContextID = "mutated"
	first, err := store.Get(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Task.ContextID != "context" {
		t.Fatalf("stored context = %q, want context", first.Task.ContextID)
	}
	first.Task.ContextID = "also-mutated"
	second, err := store.Get(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Task.ContextID != "context" {
		t.Fatalf("second get context = %q, want context", second.Task.ContextID)
	}
}

func TestLocalAuthenticationErrors(t *testing.T) {
	want := errors.New("auth unavailable")
	store, err := New("sqlite:///"+filepath.Join(t.TempDir(), "sessions.db"), func(context.Context) (string, error) {
		return "", want
	})
	if err != nil {
		t.Fatal(err)
	}
	task := testTask("task", "context", a2atype.TaskStateWorking, nil)

	if _, err := store.Create(t.Context(), task); !errors.Is(err, want) {
		t.Fatalf("create error = %v, want wrapped %v", err, want)
	}
	if _, err := store.Get(t.Context(), task.ID); !errors.Is(err, want) {
		t.Fatalf("get error = %v, want wrapped %v", err, want)
	}
	if _, err := store.List(t.Context(), &a2atype.ListTasksRequest{}); !errors.Is(err, a2atype.ErrUnauthenticated) {
		t.Fatalf("list error = %v, want %v", err, a2atype.ErrUnauthenticated)
	}
}

func TestLocalUpdate(t *testing.T) {
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	store := newTestStore(t, dbURL, "alice")
	task := testTask("task", "context", a2atype.TaskStateWorking, nil)
	if _, err := store.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}

	task.Status.State = a2atype.TaskStateCompleted
	version, err := store.Update(t.Context(), &a2ataskstore.UpdateRequest{Task: task, PrevVersion: a2ataskstore.TaskVersionMissing})
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("updated version = %d, want 2", version)
	}

	reopened := newTestStore(t, dbURL, "alice")
	stored, err := reopened.Get(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != 2 || stored.Task.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("reopened task = (%s, %d), want (%s, 2)", stored.Task.Status.State, stored.Version, a2atype.TaskStateCompleted)
	}

	missing := testTask("missing", "context", a2atype.TaskStateWorking, nil)
	if _, err := store.Update(t.Context(), &a2ataskstore.UpdateRequest{Task: missing}); !errors.Is(err, a2atype.ErrTaskNotFound) {
		t.Fatalf("missing update error = %v, want %v", err, a2atype.ErrTaskNotFound)
	}
	otherUser := newTestStore(t, dbURL, "bob")
	if _, err := otherUser.Update(t.Context(), &a2ataskstore.UpdateRequest{Task: task}); !errors.Is(err, a2atype.ErrTaskNotFound) {
		t.Fatalf("other-user update error = %v, want %v", err, a2atype.ErrTaskNotFound)
	}
}

func TestLocalList(t *testing.T) {
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	alice := newTestStore(t, dbURL, "alice")
	bob := newTestStore(t, dbURL, "bob")
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := old.Add(time.Hour)
	for _, task := range []*a2atype.Task{
		testTask("recent", "wanted", a2atype.TaskStateInputRequired, &recent),
		testTask("old", "wanted", a2atype.TaskStateInputRequired, &old),
		testTask("other-context", "other", a2atype.TaskStateWorking, nil),
	} {
		if _, err := alice.Create(t.Context(), task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bob.Create(t.Context(), testTask("bob", "wanted", a2atype.TaskStateInputRequired, &recent)); err != nil {
		t.Fatal(err)
	}

	historyLength := 2
	response, err := alice.List(t.Context(), &a2atype.ListTasksRequest{
		ContextID: "wanted", Status: a2atype.TaskStateInputRequired,
		StatusTimestampAfter: &recent, HistoryLength: &historyLength,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.TotalSize != 1 || len(response.Tasks) != 1 || response.Tasks[0].ID != "recent" {
		t.Fatalf("filtered tasks = %v (total %d), want recent", response.Tasks, response.TotalSize)
	}
	if len(response.Tasks[0].History) != 2 || response.Tasks[0].History[0].ID != "recent-message-2" {
		t.Fatalf("shaped history = %v, want last two messages", response.Tasks[0].History)
	}
	if response.Tasks[0].Artifacts != nil {
		t.Fatalf("artifacts = %v, want omitted", response.Tasks[0].Artifacts)
	}

	withArtifacts, err := alice.List(t.Context(), &a2atype.ListTasksRequest{ContextID: "wanted", IncludeArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if withArtifacts.TotalSize != 2 || len(withArtifacts.Tasks[0].Artifacts) != 1 {
		t.Fatalf("artifact response = %#v, want two owned tasks with artifacts", withArtifacts)
	}

	var ids []string
	token := ""
	for {
		page, err := alice.List(t.Context(), &a2atype.ListTasksRequest{PageSize: 1, PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalSize != 3 || len(page.Tasks) != 1 {
			t.Fatalf("page = %#v, want one of three owned tasks", page)
		}
		ids = append(ids, string(page.Tasks[0].ID))
		if len(ids) > 3 {
			t.Fatal("pagination did not terminate")
		}
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"old", "other-context", "recent"}) {
		t.Fatalf("paged IDs = %v", ids)
	}

	for _, test := range []struct {
		request *a2atype.ListTasksRequest
		want    error
	}{
		{&a2atype.ListTasksRequest{PageSize: -1}, a2atype.ErrInvalidRequest},
		{&a2atype.ListTasksRequest{PageSize: 101}, a2atype.ErrInvalidRequest},
		{&a2atype.ListTasksRequest{PageToken: "invalid"}, a2atype.ErrParseError},
	} {
		if _, err := alice.List(t.Context(), test.request); !errors.Is(err, test.want) {
			t.Fatalf("List(%+v) error = %v, want %v", test.request, err, test.want)
		}
	}
}

func TestPathFromSessionDBURL(t *testing.T) {
	for _, test := range []struct {
		url, want string
	}{
		{"sqlite:////data/sessions.db", "/data/tasks.db"},
		{"sqlite:///data/sessions.db", "/data/tasks.db"},
		{"sqlite+pysqlite:////data/sessions.db", "/data/tasks.db"},
	} {
		t.Run(test.url, func(t *testing.T) {
			got, err := pathFromSessionDBURL(test.url)
			if err != nil || got != test.want {
				t.Fatalf("pathFromSessionDBURL(%q) = (%q, %v), want (%q, nil)", test.url, got, err, test.want)
			}
		})
	}
	for _, url := range []string{"", "postgres:///data/sessions.db", "sqlite://"} {
		t.Run("invalid_"+url, func(t *testing.T) {
			if _, err := pathFromSessionDBURL(url); err == nil {
				t.Fatalf("pathFromSessionDBURL(%q) succeeded, want error", url)
			}
		})
	}
}
