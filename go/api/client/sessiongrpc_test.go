package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recordingSessionService struct {
	apiv1alpha1.UnimplementedSessionServiceServer

	mu            sync.Mutex
	observations  []callObservation
	createRequest *apiv1alpha1.CreateSessionRequest
	updateRequest *apiv1alpha1.UpdateSessionRequest
}

func (service *recordingSessionService) ListSessions(ctx context.Context, _ *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error) {
	service.observe(ctx)
	return &apiv1alpha1.ListSessionsResponse{Sessions: []*apiv1alpha1.Session{sessionClientTestSession()}}, nil
}

func (service *recordingSessionService) CreateSession(ctx context.Context, request *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
	service.observe(ctx)
	service.mu.Lock()
	service.createRequest = request
	service.mu.Unlock()
	return &apiv1alpha1.CreateSessionResponse{Session: sessionClientTestSession()}, nil
}

func (service *recordingSessionService) GetSession(ctx context.Context, _ *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
	service.observe(ctx)
	return &apiv1alpha1.GetSessionResponse{Session: sessionClientTestSession()}, nil
}

func (service *recordingSessionService) UpdateSession(ctx context.Context, request *apiv1alpha1.UpdateSessionRequest) (*apiv1alpha1.UpdateSessionResponse, error) {
	service.observe(ctx)
	service.mu.Lock()
	service.updateRequest = request
	service.mu.Unlock()
	return &apiv1alpha1.UpdateSessionResponse{Session: sessionClientTestSession()}, nil
}

func (service *recordingSessionService) DeleteSession(ctx context.Context, _ *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error) {
	service.observe(ctx)
	return &apiv1alpha1.DeleteSessionResponse{}, nil
}

func (service *recordingSessionService) observe(ctx context.Context) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.observations = append(service.observations, callObservation{
		userID:      first(values.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

type recordingTaskService struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	observation callObservation
}

func (service *recordingTaskService) ListTasks(ctx context.Context, _ *apiv1alpha1.ListTasksRequest) (*apiv1alpha1.ListTasksResponse, error) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	service.observation = callObservation{userID: first(values.Get("x-user-id")), hasDeadline: hasDeadline}
	task := &a2a.Task{
		ID:        "task-1",
		ContextID: "session-1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	}
	encoded, err := pbconv.ToProtoTask(task)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ListTasksResponse{Tasks: []*a2apb.Task{encoded}}, nil
}

func TestSessionClientUsesGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	sessionService := &recordingSessionService{}
	taskService := &recordingTaskService{}
	server := grpc.NewServer()
	apiv1alpha1.RegisterSessionServiceServer(server, sessionService)
	apiv1alpha1.RegisterTaskStoreServiceServer(server, taskService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("session-user"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	listed, err := clientSet.Session.ListSessions(t.Context())
	require.NoError(t, err)
	require.Len(t, listed.Data, 1)
	assert.Equal(t, "session-1", listed.Data[0].ID)
	assert.Equal(t, database.SessionSourceAgent, *listed.Data[0].Source)
	assert.Equal(t, time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC), listed.Data[0].CreatedAt)

	name := "Created"
	agentRef := "default/agent"
	source := database.SessionSourceAgent
	created, err := clientSet.Session.CreateSession(t.Context(), &api.SessionRequest{
		ID:       new("session-1"),
		Name:     &name,
		AgentRef: &agentRef,
		Source:   &source,
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", created.Data.ID)

	got, err := clientSet.Session.GetSession(t.Context(), "session-1")
	require.NoError(t, err)
	assert.Equal(t, "session-user", got.Data.UserID)

	updatedName := "Updated"
	updated, err := clientSet.Session.UpdateSession(t.Context(), &api.SessionRequest{
		ID:   new("session-1"),
		Name: &updatedName,
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", updated.Data.ID)
	require.NoError(t, clientSet.Session.DeleteSession(t.Context(), "session-1"))

	runs, err := clientSet.Session.ListSessionRuns(t.Context(), "session-1")
	require.NoError(t, err)
	tasks, ok := runs.Data.([]*a2a.Task)
	require.True(t, ok)
	require.Len(t, tasks, 1)
	assert.Equal(t, a2a.TaskID("task-1"), tasks[0].ID)
	assert.Equal(t, a2a.TaskStateWorking, tasks[0].Status.State)

	sessionService.mu.Lock()
	require.Len(t, sessionService.observations, 5)
	for _, observation := range sessionService.observations {
		assert.Equal(t, callObservation{userID: "session-user", hasDeadline: true}, observation)
	}
	require.NotNil(t, sessionService.createRequest)
	assert.Equal(t, apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT, sessionService.createRequest.GetSource())
	assert.Equal(t, "default/agent", sessionService.createRequest.GetAgentRef())
	require.NotNil(t, sessionService.updateRequest)
	assert.Equal(t, "session-1", sessionService.updateRequest.GetSessionId())
	assert.Equal(t, "Updated", sessionService.updateRequest.GetName())
	assert.Nil(t, sessionService.updateRequest.AgentRef)
	sessionService.mu.Unlock()

	assert.Equal(t, callObservation{userID: "session-user", hasDeadline: true}, taskService.observation)
	assert.Equal(t, int32(1), dialCount.Load())
}

func sessionClientTestSession() *apiv1alpha1.Session {
	source := apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT
	return &apiv1alpha1.Session{
		Id:        "session-1",
		Name:      new("Chat"),
		UserId:    "session-user",
		AgentId:   new("default__NS__agent"),
		Source:    &source,
		CreatedAt: timestamppb.New(time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)),
		UpdatedAt: timestamppb.New(time.Date(2026, time.August, 4, 9, 5, 0, 0, time.UTC)),
	}
}
