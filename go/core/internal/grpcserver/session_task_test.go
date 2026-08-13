package grpcserver

import (
	"cmp"
	"context"
	"net"
	"slices"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	sessionservice "github.com/kagent-dev/kagent/go/core/internal/service/session"
	taskservice "github.com/kagent-dev/kagent/go/core/internal/service/task"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type generatedClientSessionTaskStore struct {
	database.Client
	agents                map[string]*database.Agent
	sessions              map[string]*database.Session
	events                map[string][]*database.Event
	shares                map[string]*database.SessionShare
	tasks                 map[string]*a2a.Task
	taskOwners            map[string]string
	lastEventQueryOptions database.QueryOptions
	lastEventUserID       string
	lastTaskListUserID    string
	recordedShareUserID   string
	recordedShareID       int64
}

func newGeneratedClientSessionTaskStore() *generatedClientSessionTaskStore {
	return &generatedClientSessionTaskStore{
		agents:     make(map[string]*database.Agent),
		sessions:   make(map[string]*database.Session),
		events:     make(map[string][]*database.Event),
		shares:     make(map[string]*database.SessionShare),
		tasks:      make(map[string]*a2a.Task),
		taskOwners: make(map[string]string),
	}
}

func (s *generatedClientSessionTaskStore) StoreSession(_ context.Context, value *database.Session) error {
	copy := *value
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	}
	copy.UpdatedAt = copy.CreatedAt.Add(time.Minute)
	s.sessions[value.ID] = &copy
	return nil
}

func (s *generatedClientSessionTaskStore) GetSession(_ context.Context, id, userID string) (*database.Session, error) {
	value, ok := s.sessions[id]
	if !ok || value.UserID != userID {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *generatedClientSessionTaskStore) ListSessions(_ context.Context, userID string) ([]database.Session, error) {
	result := make([]database.Session, 0)
	for _, value := range s.sessions {
		if value.UserID == userID {
			result = append(result, *value)
		}
	}
	slices.SortFunc(result, func(a, b database.Session) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return result, nil
}

func (s *generatedClientSessionTaskStore) ListSessionsForAgent(_ context.Context, agentID, userID string) ([]database.SessionWithShareToken, error) {
	result := make([]database.SessionWithShareToken, 0)
	for _, value := range s.sessions {
		if value.UserID == userID && value.AgentID != nil && *value.AgentID == agentID {
			result = append(result, database.SessionWithShareToken{Session: *value})
		}
	}
	return result, nil
}

func (s *generatedClientSessionTaskStore) ListSessionsForAgentAllUsers(_ context.Context, agentID string) ([]database.Session, error) {
	result := make([]database.Session, 0)
	for _, value := range s.sessions {
		if value.AgentID != nil && *value.AgentID == agentID {
			result = append(result, *value)
		}
	}
	return result, nil
}

func (s *generatedClientSessionTaskStore) DeleteSession(_ context.Context, id, userID string) error {
	value, ok := s.sessions[id]
	if !ok || value.UserID != userID {
		return database.ErrNotFound
	}
	delete(s.sessions, id)
	return nil
}

func (s *generatedClientSessionTaskStore) GetAgent(_ context.Context, id string) (*database.Agent, error) {
	value, ok := s.agents[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *generatedClientSessionTaskStore) StoreEvents(_ context.Context, values ...*database.Event) error {
	for _, value := range values {
		copy := *value
		copy.CreatedAt = time.Date(2026, time.August, 2, 11, 0, 0, 0, time.UTC)
		copy.UpdatedAt = copy.CreatedAt
		s.events[value.SessionID] = append(s.events[value.SessionID], &copy)
	}
	return nil
}

func (s *generatedClientSessionTaskStore) ListEventsForSession(_ context.Context, sessionID, userID string, options database.QueryOptions) ([]*database.Event, error) {
	s.lastEventQueryOptions = options
	s.lastEventUserID = userID
	return s.events[sessionID], nil
}

func (s *generatedClientSessionTaskStore) CreateSessionShare(_ context.Context, value *database.SessionShare) (*database.SessionShare, error) {
	copy := *value
	copy.ID = int64(len(s.shares) + 1)
	copy.CreatedAt = time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	s.shares[value.Token] = &copy
	return &copy, nil
}

func (s *generatedClientSessionTaskStore) GetSessionShareByToken(_ context.Context, token string) (*database.SessionShare, error) {
	value, ok := s.shares[token]
	if !ok {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *generatedClientSessionTaskStore) ListSessionSharesBySession(_ context.Context, sessionID string) ([]database.SessionShare, error) {
	result := make([]database.SessionShare, 0)
	for _, value := range s.shares {
		if value.SessionID == sessionID {
			result = append(result, *value)
		}
	}
	return result, nil
}

func (s *generatedClientSessionTaskStore) DeleteSessionShare(_ context.Context, token, sessionID, userID string) error {
	value, ok := s.shares[token]
	if ok && value.SessionID == sessionID && value.UserID == userID {
		delete(s.shares, token)
	}
	return nil
}

func (s *generatedClientSessionTaskStore) RecordShareAccess(_ context.Context, userID string, shareID int64) error {
	s.recordedShareUserID = userID
	s.recordedShareID = shareID
	return nil
}

func (s *generatedClientSessionTaskStore) StoreTask(_ context.Context, value *a2a.Task, userID string) error {
	if owner, ok := s.taskOwners[string(value.ID)]; ok && owner != userID {
		return database.ErrTaskOwnedByAnotherUser
	}
	copy := *value
	s.tasks[string(value.ID)] = &copy
	s.taskOwners[string(value.ID)] = userID
	return nil
}

func (s *generatedClientSessionTaskStore) GetTask(_ context.Context, id, userID string) (*a2a.Task, error) {
	value, ok := s.tasks[id]
	if !ok || s.taskOwners[id] != userID {
		return nil, database.ErrNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *generatedClientSessionTaskStore) DeleteTask(_ context.Context, id, userID string) error {
	owner, ok := s.taskOwners[id]
	if !ok {
		return nil
	}
	if owner != userID {
		return database.ErrTaskOwnedByAnotherUser
	}
	delete(s.tasks, id)
	delete(s.taskOwners, id)
	return nil
}

func (s *generatedClientSessionTaskStore) ListTasksForSession(_ context.Context, sessionID, userID string) ([]*a2a.Task, error) {
	s.lastTaskListUserID = userID
	result := make([]*a2a.Task, 0)
	for id, value := range s.tasks {
		if value.ContextID == sessionID && s.taskOwners[id] == userID {
			copy := *value
			result = append(result, &copy)
		}
	}
	return result, nil
}

func TestSessionAndTaskGeneratedClients(t *testing.T) {
	store := newGeneratedClientSessionTaskStore()
	agentID := utils.ConvertToPythonIdentifier("default/agent")
	store.agents[agentID] = &database.Agent{ID: agentID}
	sessionService := sessionservice.NewService(
		store,
		sessionservice.WithShareTokenGenerator(func() (string, error) { return "generated-share-token", nil }),
	)
	taskService := taskservice.NewService(store)

	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:       listener,
		Registerer:     prometheus.NewRegistry(),
		Authenticator:  &authimpl.UnsecureAuthenticator{},
		ShareStore:     store,
		SessionService: sessionService,
		TaskService:    taskService,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancelServer()
		if err := <-done; err != nil {
			t.Errorf("gRPC server shutdown error = %v", err)
		}
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	userContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-a"))
	sessionClient := apiv1alpha1.NewSessionServiceClient(connection)
	source := apiv1alpha1.SessionSource_SESSION_SOURCE_USER
	sessionID := "session-1"
	name := "Initial name"
	created, err := sessionClient.CreateSession(userContext, &apiv1alpha1.CreateSessionRequest{
		Id:       &sessionID,
		AgentRef: "default/agent",
		Name:     &name,
		Source:   &source,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if created.GetSession().GetId() != sessionID || created.GetSession().GetUserId() != "user-a" || created.GetSession().GetSource() != source {
		t.Fatalf("CreateSession() = %+v", created.GetSession())
	}
	if created.GetSession().GetCreatedAt() == nil || created.GetSession().GetUpdatedAt() == nil {
		t.Fatalf("CreateSession() timestamps = %+v", created.GetSession())
	}

	listed, err := sessionClient.ListSessions(userContext, &apiv1alpha1.ListSessionsRequest{})
	if err != nil || len(listed.GetSessions()) != 1 {
		t.Fatalf("ListSessions() = %+v, error = %v", listed, err)
	}
	listedByAgent, err := sessionClient.ListSessionsByAgent(userContext, &apiv1alpha1.ListSessionsByAgentRequest{
		AgentRef: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "agent"},
	})
	if err != nil || len(listedByAgent.GetSessions()) != 1 {
		t.Fatalf("ListSessionsByAgent() = %+v, error = %v", listedByAgent, err)
	}

	agentContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		"x-user-id", "user-a",
		"x-agent-name", "default/agent",
	))
	if _, err := sessionClient.AddSessionEvent(agentContext, &apiv1alpha1.AddSessionEventRequest{
		SessionId: sessionID,
		Id:        "event-1",
		Data:      `{"type":"message"}`,
	}); err != nil {
		t.Fatalf("AddSessionEvent() error = %v", err)
	}

	after := time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)
	limit := int32(20)
	gotSession, err := sessionClient.GetSession(userContext, &apiv1alpha1.GetSessionRequest{
		SessionId: sessionID,
		Order:     apiv1alpha1.EventOrder_EVENT_ORDER_ASCENDING,
		After:     timestamppb.New(after),
		Limit:     &limit,
	})
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(gotSession.GetEvents()) != 1 || gotSession.GetEvents()[0].GetData() != `{"type":"message"}` {
		t.Fatalf("GetSession().events = %+v", gotSession.GetEvents())
	}
	if !store.lastEventQueryOptions.OrderAsc || store.lastEventQueryOptions.Limit != 20 || !store.lastEventQueryOptions.After.Equal(after) {
		t.Fatalf("GetSession() options = %+v", store.lastEventQueryOptions)
	}

	share, err := sessionClient.CreateSessionShare(userContext, &apiv1alpha1.CreateSessionShareRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("CreateSessionShare() error = %v", err)
	}
	if share.GetShare().GetToken() != "generated-share-token" || !share.GetShare().GetReadOnly() || share.GetShare().GetCreatedAt() == nil {
		t.Fatalf("CreateSessionShare() = %+v", share.GetShare())
	}
	shares, err := sessionClient.ListSessionShares(userContext, &apiv1alpha1.ListSessionSharesRequest{SessionId: sessionID})
	if err != nil || len(shares.GetShares()) != 1 {
		t.Fatalf("ListSessionShares() = %+v, error = %v", shares, err)
	}

	taskClient := apiv1alpha1.NewTaskStoreServiceClient(connection)
	taskValue := &a2a.Task{
		ID:        "task-1",
		ContextID: sessionID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	}
	taskObject, err := pbconv.ToProtoTask(taskValue)
	if err != nil {
		t.Fatalf("pbconv.ToProtoTask() error = %v", err)
	}
	_, err = taskClient.UpsertTask(userContext, &apiv1alpha1.UpsertTaskRequest{Task: taskObject})
	if err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	gotTask, err := taskClient.GetTask(userContext, &apiv1alpha1.GetTaskRequest{TaskId: "task-1"})
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	assertTaskObject(t, gotTask.GetTask(), "task-1", sessionID)

	visitorContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		"x-user-id", "visitor",
		"x-share-token", "generated-share-token",
	))
	sharedSession, err := sessionClient.GetSession(visitorContext, &apiv1alpha1.GetSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("GetSession(shared) error = %v", err)
	}
	if sharedSession.ReadOnly == nil || !sharedSession.GetReadOnly() || store.lastEventUserID != "user-a" {
		t.Fatalf("GetSession(shared) = %+v, event user = %q", sharedSession, store.lastEventUserID)
	}
	sharedTasks, err := taskClient.ListTasks(visitorContext, &apiv1alpha1.ListTasksRequest{SessionId: sessionID})
	if err != nil || len(sharedTasks.GetTasks()) != 1 || store.lastTaskListUserID != "user-a" {
		t.Fatalf("ListTasks(shared) = %+v, user = %q, error = %v", sharedTasks, store.lastTaskListUserID, err)
	}
	if store.recordedShareUserID != "visitor" || store.recordedShareID != share.GetShare().GetId() {
		t.Fatalf("share access = user %q, id %d", store.recordedShareUserID, store.recordedShareID)
	}

	updatedName := "Renamed"
	_, err = sessionClient.UpdateSession(visitorContext, &apiv1alpha1.UpdateSessionRequest{SessionId: sessionID, Name: &updatedName})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("UpdateSession(read-only share) error = %v, want PermissionDenied", err)
	}
	updated, err := sessionClient.UpdateSession(userContext, &apiv1alpha1.UpdateSessionRequest{SessionId: sessionID, Name: &updatedName})
	if err != nil || updated.GetSession().GetName() != updatedName {
		t.Fatalf("UpdateSession() = %+v, error = %v", updated, err)
	}

	otherContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-b"))
	otherSessions, err := sessionClient.ListSessions(otherContext, &apiv1alpha1.ListSessionsRequest{})
	if err != nil || len(otherSessions.GetSessions()) != 0 {
		t.Fatalf("ListSessions(other user) = %+v, error = %v", otherSessions, err)
	}

	if _, err := taskClient.DeleteTask(userContext, &apiv1alpha1.DeleteTaskRequest{TaskId: "task-1"}); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	_, err = taskClient.GetTask(userContext, &apiv1alpha1.GetTaskRequest{TaskId: "task-1"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetTask(deleted) error = %v, want NotFound", err)
	}
	if _, err := sessionClient.DeleteSessionShare(userContext, &apiv1alpha1.DeleteSessionShareRequest{
		SessionId: sessionID,
		Token:     "generated-share-token",
	}); err != nil {
		t.Fatalf("DeleteSessionShare() error = %v", err)
	}
	if _, err := sessionClient.DeleteSession(userContext, &apiv1alpha1.DeleteSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
}

func assertTaskObject(t *testing.T, object *a2apb.Task, taskID, contextID string) {
	t.Helper()
	decoded, err := pbconv.FromProtoTask(object)
	if err != nil {
		t.Fatalf("pbconv.FromProtoTask() error = %v", err)
	}
	if decoded.ID != a2a.TaskID(taskID) || decoded.ContextID != contextID || decoded.Status.State != a2a.TaskStateWorking {
		t.Fatalf("decoded task = %+v", decoded)
	}
}
