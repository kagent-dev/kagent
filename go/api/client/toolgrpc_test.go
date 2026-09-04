package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type recordingToolService struct {
	apiv1alpha1.UnimplementedToolServiceServer

	mu            sync.Mutex
	observations  []callObservation
	deleteRequest *apiv1alpha1.DeleteToolServerRequest
	tool          *apiv1alpha1.Tool
}

func (s *recordingToolService) observe(ctx context.Context) {
	metadataValues, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.observations = append(s.observations, callObservation{
		userID:      first(metadataValues.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

func (s *recordingToolService) ListTools(ctx context.Context, _ *apiv1alpha1.ListToolsRequest) (*apiv1alpha1.ListToolsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	return &apiv1alpha1.ListToolsResponse{Tools: []*apiv1alpha1.Tool{s.tool}}, nil
}

func (s *recordingToolService) ListToolServers(ctx context.Context, _ *apiv1alpha1.ListToolServersRequest) (*apiv1alpha1.ListToolServersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	return &apiv1alpha1.ListToolServersResponse{ToolServers: []*apiv1alpha1.ToolServer{{
		Ref:       "default/remote",
		GroupKind: "RemoteMCPServer.kagent.dev",
		DiscoveredTools: []*apiv1alpha1.DiscoveredTool{{
			Name:        "move_task",
			Description: "Move a task",
		}},
	}}}, nil
}

func (s *recordingToolService) DeleteToolServer(ctx context.Context, request *apiv1alpha1.DeleteToolServerRequest) (*apiv1alpha1.DeleteToolServerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.deleteRequest = request
	return &apiv1alpha1.DeleteToolServerResponse{}, nil
}

func TestToolClientsUseGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	service := &recordingToolService{tool: testToolMessage(t)}
	server := grpc.NewServer()
	apiv1alpha1.RegisterToolServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("test-user"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	tools, err := clientSet.Tool.ListTools(t.Context())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "move_task", tools[0].ID)
	assert.Equal(t, "default/remote", tools[0].ServerName)

	servers, err := clientSet.ToolServer.ListToolServers(t.Context())
	require.NoError(t, err)
	require.Len(t, servers, 1)
	assert.Equal(t, "default/remote", servers[0].Ref)
	assert.Equal(t, "RemoteMCPServer.kagent.dev", servers[0].GroupKind)
	require.Len(t, servers[0].DiscoveredTools, 1)
	assert.Equal(t, "move_task", servers[0].DiscoveredTools[0].Name)

	require.NoError(t, clientSet.ToolServer.DeleteToolServer(t.Context(), "default", "remote"))

	service.mu.Lock()
	defer service.mu.Unlock()

	assert.True(t, proto.Equal(&apiv1alpha1.DeleteToolServerRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "remote"},
	}, service.deleteRequest))
	require.Len(t, service.observations, 3)
	for _, observation := range service.observations {
		assert.Equal(t, "test-user", observation.userID)
		assert.True(t, observation.hasDeadline)
	}
	assert.Equal(t, int32(1), dialCount.Load())
}

func TestToolClientsValidateRequestsBeforeCallingServer(t *testing.T) {
	clientSet := New("http://unused.invalid", WithGRPCTarget(""), WithUserID("test-user"))
	t.Cleanup(func() { _ = clientSet.Close() })

	err := clientSet.ToolServer.DeleteToolServer(t.Context(), "", "name")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func testToolMessage(t *testing.T) *apiv1alpha1.Tool {
	t.Helper()
	resource, err := structuredobject.FromGo(&dbpkg.Tool{
		ID:          "move_task",
		ServerName:  "default/remote",
		GroupKind:   "RemoteMCPServer.kagent.dev",
		Description: "Move a task",
	}, "kagent.api/v1alpha1", clientToolKind, defaultGRPCMaxMessageSize)
	require.NoError(t, err)
	return &apiv1alpha1.Tool{Resource: resource}
}
