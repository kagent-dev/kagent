package task

import (
	"context"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testAuthSession struct {
	principal auth.Principal
}

func (s testAuthSession) Principal() auth.Principal {
	return s.principal
}

type taskTestStore struct {
	tasks              map[string]*a2a.Task
	sessions           map[string]*database.Session
	storeError         error
	deleteError        error
	lastTaskUserID     string
	lastSessionUserID  string
	lastTaskListUserID string
}

func newTaskTestStore() *taskTestStore {
	return &taskTestStore{tasks: make(map[string]*a2a.Task), sessions: make(map[string]*database.Session)}
}

func (s *taskTestStore) StoreTask(_ context.Context, value *a2a.Task, userID string) error {
	s.lastTaskUserID = userID
	if s.storeError != nil {
		return s.storeError
	}
	copy := *value
	s.tasks[string(value.ID)] = &copy
	return nil
}

func (s *taskTestStore) GetTask(_ context.Context, id, userID string) (*a2a.Task, error) {
	s.lastTaskUserID = userID
	value, ok := s.tasks[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *taskTestStore) DeleteTask(_ context.Context, id, userID string) error {
	s.lastTaskUserID = userID
	if s.deleteError != nil {
		return s.deleteError
	}
	delete(s.tasks, id)
	return nil
}

func (s *taskTestStore) GetSession(_ context.Context, id, userID string) (*database.Session, error) {
	s.lastSessionUserID = userID
	value, ok := s.sessions[id]
	if !ok || value.UserID != userID {
		return nil, database.ErrNotFound
	}
	return value, nil
}

func (s *taskTestStore) ListTasksForSession(_ context.Context, _ string, userID string) ([]*a2a.Task, error) {
	s.lastTaskListUserID = userID
	result := make([]*a2a.Task, 0, len(s.tasks))
	for _, value := range s.tasks {
		result = append(result, value)
	}
	return result, nil
}

func taskContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), testAuthSession{principal: auth.Principal{User: auth.User{ID: userID}}})
}

func TestCreateUsesAuthenticatedUserAndGeneratesID(t *testing.T) {
	store := newTaskTestStore()
	value := &a2a.Task{}
	created, err := NewService(store).Create(taskContext("user-a"), value)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" || store.lastTaskUserID != "user-a" {
		t.Fatalf("Create() = %+v, user = %q", created, store.lastTaskUserID)
	}
}

func TestCreateMapsOwnerConflict(t *testing.T) {
	store := newTaskTestStore()
	store.storeError = database.ErrTaskOwnedByAnotherUser
	_, err := NewService(store).Create(taskContext("user-a"), &a2a.Task{ID: "task-1"})
	if !serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists) {
		t.Fatalf("Create() error = %v, want already exists", err)
	}
}

func TestListUsesShareOwner(t *testing.T) {
	store := newTaskTestStore()
	store.sessions["shared"] = &database.Session{ID: "shared", UserID: "owner"}
	store.tasks["task-1"] = &a2a.Task{ID: "task-1"}
	ctx := taskContext("visitor")
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: "shared", UserID: "owner", ReadOnly: true})

	listed, err := NewService(store).List(ctx, "shared")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || store.lastSessionUserID != "owner" || store.lastTaskListUserID != "owner" {
		t.Fatalf("List() = %+v, users = %q/%q", listed, store.lastSessionUserID, store.lastTaskListUserID)
	}
}

func TestDeleteHidesOtherOwner(t *testing.T) {
	store := newTaskTestStore()
	store.deleteError = database.ErrTaskOwnedByAnotherUser
	err := NewService(store).Delete(taskContext("user-a"), "task-1")
	if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
		t.Fatalf("Delete() error = %v, want not found", err)
	}
}
