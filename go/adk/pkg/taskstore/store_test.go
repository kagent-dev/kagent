package taskstore

import (
	"context"
	"net"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type taskTestServer struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	upsert func(context.Context, *apiv1alpha1.UpsertTaskRequest) (*apiv1alpha1.UpsertTaskResponse, error)
	get    func(context.Context, *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error)
}

func (server *taskTestServer) UpsertTask(ctx context.Context, request *apiv1alpha1.UpsertTaskRequest) (*apiv1alpha1.UpsertTaskResponse, error) {
	return server.upsert(ctx, request)
}

func (server *taskTestServer) GetTask(ctx context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
	return server.get(ctx, request)
}

func newTaskStore(t *testing.T, service *taskTestServer) *KAgentTaskStore {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterTaskStoreServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()

	client, err := controllerclient.New(controllerclient.Config{
		Target: "passthrough:///bufnet",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	})
	return NewKAgentTaskStore(client)
}

func TestSaveUsesCanonicalTaskGRPCAndCleansPartialValues(t *testing.T) {
	service := newTaskStore(t, &taskTestServer{upsert: func(ctx context.Context, request *apiv1alpha1.UpsertTaskRequest) (*apiv1alpha1.UpsertTaskResponse, error) {
		values, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"task-user"}, values.Get("x-user-id"))

		decoded, err := pbconv.FromProtoTask(request.GetTask())
		require.NoError(t, err)
		assert.Equal(t, a2a.TaskID("task-1"), decoded.ID)
		require.Len(t, decoded.History, 1)
		assert.Equal(t, "complete-message", decoded.History[0].ID)
		require.Len(t, decoded.Artifacts, 1)
		assert.Equal(t, a2a.ArtifactID("complete-artifact"), decoded.Artifacts[0].ID)
		return &apiv1alpha1.UpsertTaskResponse{}, nil
	}})

	completeMessage := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("keep"))
	completeMessage.ID = "complete-message"
	partialMessage := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("drop"))
	partialMessage.Metadata = map[string]any{metadataKeyKagentAdkPartial: true}
	task := &a2a.Task{
		ID:        a2a.TaskID("task-1"),
		ContextID: "session-1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		History:   []*a2a.Message{completeMessage, partialMessage, nil},
		Artifacts: []*a2a.Artifact{
			{ID: a2a.ArtifactID("complete-artifact"), Parts: a2a.ContentParts{a2a.NewTextPart("keep")}},
			{ID: a2a.ArtifactID("partial-artifact"), Parts: a2a.ContentParts{a2a.NewTextPart("drop")}, Metadata: map[string]any{metadataKeyAdkPartial: true}},
		},
	}

	version, err := service.Create(auth.WithUserID(t.Context(), "task-user"), task)
	require.NoError(t, err)
	assert.Equal(t, a2ataskstore.TaskVersionMissing, version)
	assert.Len(t, task.History, 3)
	assert.Len(t, task.Artifacts, 2)
}

func TestGetDecodesCanonicalTask(t *testing.T) {
	canonical := &a2a.Task{
		ID:        a2a.TaskID("task-2"),
		ContextID: "session-2",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		History: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done")),
		},
	}
	encoded, err := pbconv.ToProtoTask(canonical)
	require.NoError(t, err)
	service := newTaskStore(t, &taskTestServer{get: func(_ context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
		assert.Equal(t, "task-2", request.GetTaskId())
		return &apiv1alpha1.GetTaskResponse{Task: encoded}, nil
	}})

	stored, err := service.Get(t.Context(), a2a.TaskID("task-2"))
	require.NoError(t, err)
	assert.Equal(t, a2ataskstore.TaskVersionMissing, stored.Version)
	assert.Equal(t, a2a.TaskID("task-2"), stored.Task.ID)
	assert.Equal(t, a2a.TaskStateCompleted, stored.Task.Status.State)
	require.Len(t, stored.Task.History, 1)
	assert.Equal(t, "done", stored.Task.History[0].Parts[0].Text())
}

func TestGetMapsNotFound(t *testing.T) {
	service := newTaskStore(t, &taskTestServer{get: func(context.Context, *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
		return nil, status.Error(codes.NotFound, "missing")
	}})

	stored, err := service.Get(t.Context(), a2a.TaskID("missing"))
	require.ErrorIs(t, err, a2a.ErrTaskNotFound)
	assert.Nil(t, stored)
}

func TestSaveRejectsNilTask(t *testing.T) {
	service := &KAgentTaskStore{}
	_, err := service.Create(t.Context(), nil)
	require.EqualError(t, err, "task cannot be nil")
}
