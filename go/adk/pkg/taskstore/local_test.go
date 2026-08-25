package taskstore

import (
	"context"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

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
