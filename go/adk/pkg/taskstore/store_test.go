package taskstore

import (
	"context"
	"net"
	"testing"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type taskTestServer struct {
	apiv1alpha1.UnimplementedTaskServiceServer
	create func(context.Context, *apiv1alpha1.CreateTaskRequest) (*apiv1alpha1.CreateTaskResponse, error)
	get    func(context.Context, *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error)
}

func (server *taskTestServer) CreateTask(ctx context.Context, request *apiv1alpha1.CreateTaskRequest) (*apiv1alpha1.CreateTaskResponse, error) {
	return server.create(ctx, request)
}

func (server *taskTestServer) GetTask(ctx context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
	return server.get(ctx, request)
}

func newTaskStore(t *testing.T, service *taskTestServer) *KAgentTaskStore {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterTaskServiceServer(server, service)
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
	service := newTaskStore(t, &taskTestServer{create: func(ctx context.Context, request *apiv1alpha1.CreateTaskRequest) (*apiv1alpha1.CreateTaskResponse, error) {
		assert.Equal(t, a2aTaskAPIVersion, request.GetTask().GetApiVersion())
		assert.Equal(t, a2aTaskKind, request.GetTask().GetKind())
		values, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"task-user"}, values.Get("x-user-id"))

		decoded := new(a2a.Task)
		require.NoError(t, structuredobject.ToGo(request.GetTask(), a2aTaskKind, decoded, 16<<20))
		assert.Equal(t, a2a.TaskID("task-1"), decoded.ID)
		require.Len(t, decoded.History, 1)
		assert.Equal(t, "complete-message", decoded.History[0].ID)
		require.Len(t, decoded.Artifacts, 1)
		assert.Equal(t, a2a.ArtifactID("complete-artifact"), decoded.Artifacts[0].ID)
		return &apiv1alpha1.CreateTaskResponse{Task: request.GetTask()}, nil
	}})

	completeMessage := legacya2a.NewMessage(legacya2a.MessageRoleUser, legacya2a.TextPart{Text: "keep"})
	completeMessage.ID = "complete-message"
	partialMessage := legacya2a.NewMessage(legacya2a.MessageRoleAgent, legacya2a.TextPart{Text: "drop"})
	partialMessage.Metadata = map[string]any{metadataKeyKagentAdkPartial: true}
	task := &legacya2a.Task{
		ID:        legacya2a.TaskID("task-1"),
		ContextID: "session-1",
		Status:    legacya2a.TaskStatus{State: legacya2a.TaskStateWorking},
		History:   []*legacya2a.Message{completeMessage, partialMessage, {}},
		Artifacts: []*legacya2a.Artifact{
			{ID: legacya2a.ArtifactID("complete-artifact"), Parts: legacya2a.ContentParts{legacya2a.TextPart{Text: "keep"}}},
			{ID: legacya2a.ArtifactID("partial-artifact"), Parts: legacya2a.ContentParts{legacya2a.TextPart{Text: "drop"}}, Metadata: map[string]any{metadataKeyAdkPartial: true}},
		},
	}

	version, err := service.Save(auth.WithUserID(t.Context(), "task-user"), task, nil, nil, legacya2a.TaskVersionMissing)
	require.NoError(t, err)
	assert.Equal(t, legacya2a.TaskVersion(1), version)
	assert.Len(t, task.History, 3)
	assert.Len(t, task.Artifacts, 2)
}

func TestGetDecodesCanonicalTaskForLegacyStore(t *testing.T) {
	canonical := &a2a.Task{
		ID:        a2a.TaskID("task-2"),
		ContextID: "session-2",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		History: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done")),
		},
	}
	encoded, err := structuredobject.FromGo(canonical, a2aTaskAPIVersion, a2aTaskKind, 16<<20)
	require.NoError(t, err)
	service := newTaskStore(t, &taskTestServer{get: func(_ context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
		assert.Equal(t, "task-2", request.GetTaskId())
		return &apiv1alpha1.GetTaskResponse{Task: encoded}, nil
	}})

	task, version, err := service.Get(t.Context(), legacya2a.TaskID("task-2"))
	require.NoError(t, err)
	assert.Equal(t, legacya2a.TaskVersion(1), version)
	assert.Equal(t, legacya2a.TaskID("task-2"), task.ID)
	assert.Equal(t, legacya2a.TaskStateCompleted, task.Status.State)
	require.Len(t, task.History, 1)
	assert.Equal(t, "done", task.History[0].Parts[0].(legacya2a.TextPart).Text)
}

func TestGetMapsNotFound(t *testing.T) {
	service := newTaskStore(t, &taskTestServer{get: func(context.Context, *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
		return nil, status.Error(codes.NotFound, "missing")
	}})

	task, version, err := service.Get(t.Context(), legacya2a.TaskID("missing"))
	require.ErrorIs(t, err, legacya2a.ErrTaskNotFound)
	assert.Nil(t, task)
	assert.Equal(t, legacya2a.TaskVersionMissing, version)
}

func TestSaveRejectsNilTask(t *testing.T) {
	service := &KAgentTaskStore{}
	_, err := service.Save(t.Context(), nil, nil, nil, legacya2a.TaskVersionMissing)
	require.EqualError(t, err, "task cannot be nil")
}
