package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testAuthSession struct {
	principal auth.Principal
}

func (s testAuthSession) Principal() auth.Principal {
	return s.principal
}

type sessionTestStore struct {
	sessions             map[string]*database.Session
	agents               map[string]*database.Agent
	events               []*database.Event
	shares               []database.SessionShare
	listAllSessions      []database.Session
	lastSessionUserID    string
	lastEventUserID      string
	deleteSessionUserID  string
	deleteShareUserID    string
	storeSessionError    error
	deleteSessionError   error
	listEventsError      error
	createShareError     error
	deleteShareError     error
	listSessionsForAgent []database.SessionWithShareToken
}

func newSessionTestStore() *sessionTestStore {
	return &sessionTestStore{
		sessions: make(map[string]*database.Session),
		agents:   make(map[string]*database.Agent),
	}
}

func (s *sessionTestStore) StoreSession(_ context.Context, value *database.Session) error {
	if s.storeSessionError != nil {
		return s.storeSessionError
	}
	copy := *value
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now()
	}
	s.sessions[value.ID] = &copy
	return nil
}

func (s *sessionTestStore) GetSession(_ context.Context, id, userID string) (*database.Session, error) {
	s.lastSessionUserID = userID
	value, ok := s.sessions[id]
	if !ok || value.UserID != userID {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *sessionTestStore) ListSessions(_ context.Context, userID string) ([]database.Session, error) {
	s.lastSessionUserID = userID
	result := make([]database.Session, 0)
	for _, value := range s.sessions {
		if value.UserID == userID {
			result = append(result, *value)
		}
	}
	return result, nil
}

func (s *sessionTestStore) ListSessionsForAgent(_ context.Context, _, userID string) ([]database.SessionWithShareToken, error) {
	s.lastSessionUserID = userID
	return s.listSessionsForAgent, nil
}

func (s *sessionTestStore) ListSessionsForAgentAllUsers(context.Context, string) ([]database.Session, error) {
	return s.listAllSessions, nil
}

func (s *sessionTestStore) DeleteSession(_ context.Context, id, userID string) error {
	s.deleteSessionUserID = userID
	if s.deleteSessionError != nil {
		return s.deleteSessionError
	}
	delete(s.sessions, id)
	return nil
}

func (s *sessionTestStore) GetAgent(_ context.Context, id string) (*database.Agent, error) {
	value, ok := s.agents[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *sessionTestStore) StoreEvents(_ context.Context, values ...*database.Event) error {
	s.events = append(s.events, values...)
	return nil
}

func (s *sessionTestStore) ListEventsForSession(_ context.Context, _, userID string, _ database.QueryOptions) ([]*database.Event, error) {
	s.lastEventUserID = userID
	if s.listEventsError != nil {
		return nil, s.listEventsError
	}
	return s.events, nil
}

func (s *sessionTestStore) CreateSessionShare(_ context.Context, value *database.SessionShare) (*database.SessionShare, error) {
	if s.createShareError != nil {
		return nil, s.createShareError
	}
	copy := *value
	copy.ID = int64(len(s.shares) + 1)
	s.shares = append(s.shares, copy)
	return &copy, nil
}

func (s *sessionTestStore) ListSessionSharesBySession(_ context.Context, sessionID string) ([]database.SessionShare, error) {
	result := make([]database.SessionShare, 0)
	for _, value := range s.shares {
		if value.SessionID == sessionID {
			result = append(result, value)
		}
	}
	return result, nil
}

func (s *sessionTestStore) DeleteSessionShare(_ context.Context, token, sessionID, userID string) error {
	s.deleteShareUserID = userID
	if s.deleteShareError != nil {
		return s.deleteShareError
	}
	for index, value := range s.shares {
		if value.Token == token && value.SessionID == sessionID && value.UserID == userID {
			s.shares = append(s.shares[:index], s.shares[index+1:]...)
			break
		}
	}
	return nil
}

func sessionContext(userID, agentID string) context.Context {
	return auth.AuthSessionTo(context.Background(), testAuthSession{principal: auth.Principal{
		User:  auth.User{ID: userID},
		Agent: auth.Agent{ID: agentID},
	}})
}

func TestCreateAndListUseAuthenticatedUser(t *testing.T) {
	store := newSessionTestStore()
	store.agents["default__NS__agent"] = &database.Agent{ID: "default__NS__agent"}
	service := NewService(store)
	name := "Session name"

	created, err := service.Create(sessionContext("user-a", ""), CreateRequest{
		AgentRef: "default/agent",
		Name:     &name,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.UserID != "user-a" || created.AgentID == nil || *created.AgentID != "default__NS__agent" {
		t.Fatalf("Create() = %+v", created)
	}

	listed, err := service.List(sessionContext("user-a", ""))
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || store.lastSessionUserID != "user-a" {
		t.Fatalf("List() = %+v, user = %q", listed, store.lastSessionUserID)
	}
}

func TestCreateEnforcesLegacySandboxSingleSession(t *testing.T) {
	store := newSessionTestStore()
	store.agents["default__NS__sandbox"] = &database.Agent{ID: "default__NS__sandbox", WorkloadType: v1alpha3.WorkloadModeSandbox}
	store.listAllSessions = []database.Session{{ID: "existing"}}

	_, err := NewService(store).Create(sessionContext("user-a", ""), CreateRequest{AgentRef: "default/sandbox"})
	if !serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists) {
		t.Fatalf("Create() error = %v, want already exists", err)
	}
}

func TestGetUsesShareOwnerAndReportsReadOnly(t *testing.T) {
	store := newSessionTestStore()
	store.sessions["shared"] = &database.Session{ID: "shared", UserID: "owner"}
	store.events = []*database.Event{{ID: "event-1", SessionID: "shared", UserID: "owner"}}
	ctx := sessionContext("visitor", "")
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: "shared", UserID: "owner", ReadOnly: true})

	result, err := NewService(store).Get(ctx, "shared", database.QueryOptions{OrderAsc: true, Limit: 5})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if store.lastSessionUserID != "owner" || store.lastEventUserID != "owner" {
		t.Fatalf("Get() users = session %q, events %q", store.lastSessionUserID, store.lastEventUserID)
	}
	if result.ReadOnly == nil || !*result.ReadOnly || len(result.Events) != 1 {
		t.Fatalf("Get() = %+v", result)
	}
}

func TestAddEventEnforcesAgentOwnership(t *testing.T) {
	store := newSessionTestStore()
	agentID := "default__NS__agent"
	store.sessions["session-1"] = &database.Session{ID: "session-1", UserID: "user-a", AgentID: &agentID}
	service := NewService(store)

	_, err := service.AddEvent(sessionContext("user-a", "default/other"), AddEventRequest{SessionID: "session-1", ID: "event-1", Data: `{}`})
	if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("AddEvent(other agent) error = %v, want permission denied", err)
	}

	event, err := service.AddEvent(sessionContext("user-a", "default/agent"), AddEventRequest{SessionID: "session-1", ID: "event-1", Data: `{}`})
	if err != nil {
		t.Fatalf("AddEvent(owner agent) error = %v", err)
	}
	if event.UserID != "user-a" || len(store.events) != 1 {
		t.Fatalf("AddEvent() = %+v, stored = %+v", event, store.events)
	}
}

func TestShareOperationsAreOwnerScopedAndDefaultReadOnly(t *testing.T) {
	store := newSessionTestStore()
	store.sessions["session-1"] = &database.Session{ID: "session-1", UserID: "owner"}
	service := NewService(store, WithShareTokenGenerator(func() (string, error) { return "fixed-token", nil }))

	share, err := service.CreateShare(sessionContext("owner", ""), "session-1", nil)
	if err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if !share.ReadOnly || share.Token != "fixed-token" || share.UserID != "owner" {
		t.Fatalf("CreateShare() = %+v", share)
	}

	_, err = service.ListShares(sessionContext("visitor", ""), "session-1")
	if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
		t.Fatalf("ListShares(visitor) error = %v, want not found", err)
	}

	if err := service.DeleteShare(sessionContext("owner", ""), "session-1", "fixed-token"); err != nil {
		t.Fatalf("DeleteShare() error = %v", err)
	}
	if store.deleteShareUserID != "owner" || len(store.shares) != 0 {
		t.Fatalf("DeleteShare() user = %q, shares = %+v", store.deleteShareUserID, store.shares)
	}
}

func TestMissingAuthenticationAndStoreErrorsAreCanonical(t *testing.T) {
	service := NewService(newSessionTestStore())
	if _, err := service.List(context.Background()); !serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated) {
		t.Fatalf("List() error = %v, want unauthenticated", err)
	}

	store := newSessionTestStore()
	store.sessions["session-1"] = &database.Session{ID: "session-1", UserID: "owner"}
	store.listEventsError = errors.New("database unavailable")
	_, err := NewService(store).Get(sessionContext("owner", ""), "session-1", database.QueryOptions{})
	if !serviceerrors.IsCode(err, serviceerrors.CodeInternal) {
		t.Fatalf("Get() error = %v, want internal", err)
	}
}

func TestCreateAndUpdateMapRetiredSessionID(t *testing.T) {
	store := newSessionTestStore()
	store.agents["default__NS__agent"] = &database.Agent{ID: "default__NS__agent"}
	store.storeSessionError = database.ErrSessionIDRetired
	id := "retired-session"

	_, err := NewService(store).Create(sessionContext("user-a", ""), CreateRequest{ID: &id, AgentRef: "default/agent"})
	if !serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists) {
		t.Fatalf("Create() error = %v, want already exists", err)
	}

	store.sessions[id] = &database.Session{ID: id, UserID: "user-a"}
	name := "renamed"
	_, err = NewService(store).Update(sessionContext("user-a", ""), UpdateRequest{SessionID: id, Name: &name})
	if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
		t.Fatalf("Update() error = %v, want not found", err)
	}
}
