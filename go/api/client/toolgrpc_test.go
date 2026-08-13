package client

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	legacyv1alpha1 "github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type recordingToolService struct {
	apiv1alpha1.UnimplementedToolServiceServer

	mu             sync.Mutex
	observations   []callObservation
	createRequests []*apiv1alpha1.CreateToolServerRequest
	deleteRequest  *apiv1alpha1.DeleteToolServerRequest
	tool           *apiv1alpha1.Tool
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

func (s *recordingToolService) CreateToolServer(ctx context.Context, request *apiv1alpha1.CreateToolServerRequest) (*apiv1alpha1.CreateToolServerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.createRequests = append(s.createRequests, request)
	return &apiv1alpha1.CreateToolServerResponse{Resource: request.GetResource()}, nil
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

	terminateOnClose := false
	remoteRequest := &legacyv1alpha1.ToolServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "remote", Labels: map[string]string{"test": "true"}},
		Spec: legacyv1alpha1.ToolServerSpec{
			Description: "Remote server",
			Config: legacyv1alpha1.ToolServerConfig{
				Type: legacyv1alpha1.ToolServerTypeStreamableHttp,
				StreamableHttp: &legacyv1alpha1.StreamableHttpServerConfig{
					HttpToolServerConfig: legacyv1alpha1.HttpToolServerConfig{
						URL: "https://remote.example/mcp",
						Headers: map[string]legacyv1alpha1.AnyType{
							"Authorization": {RawMessage: json.RawMessage(`"Bearer inline"`)},
						},
						HeadersFrom: []legacyv1alpha1.ValueRef{{
							Name: "X-Token",
							ValueFrom: &legacyv1alpha1.ValueSource{
								Type:     legacyv1alpha1.SecretValueSource,
								ValueRef: "remote-token",
								Key:      "token",
							},
						}},
						Timeout:        &metav1.Duration{Duration: 11 * time.Second},
						SseReadTimeout: &metav1.Duration{Duration: 12 * time.Second},
					},
					TerminateOnClose: &terminateOnClose,
				},
			},
		},
	}
	createdRemote, err := clientSet.ToolServer.CreateToolServer(t.Context(), remoteRequest)
	require.NoError(t, err)
	assert.Equal(t, remoteRequest.Name, createdRemote.Name)
	assert.Equal(t, remoteRequest.Labels, createdRemote.Labels)

	stdioRequest := &legacyv1alpha1.ToolServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "managed"},
		Spec: legacyv1alpha1.ToolServerSpec{
			Description: "Managed server",
			Config: legacyv1alpha1.ToolServerConfig{
				Type: legacyv1alpha1.ToolServerTypeStdio,
				Stdio: &legacyv1alpha1.StdioMcpServerConfig{
					Command:            "npx",
					Args:               []string{"server-everything"},
					Env:                map[string]string{"DIRECT": "value"},
					EnvFrom:            []legacyv1alpha1.ValueRef{{Name: "INLINE", Value: "inline-value"}},
					ReadTimeoutSeconds: 15,
				},
			},
		},
	}
	createdManaged, err := clientSet.ToolServer.CreateToolServer(t.Context(), stdioRequest)
	require.NoError(t, err)
	assert.Equal(t, stdioRequest.Name, createdManaged.Name)

	require.NoError(t, clientSet.ToolServer.DeleteToolServer(t.Context(), "default", "remote"))

	service.mu.Lock()
	defer service.mu.Unlock()
	require.Len(t, service.createRequests, 2)

	remoteCreate := service.createRequests[0]
	assert.Equal(t, remoteMCPServerKind, remoteCreate.GetType())
	assert.True(t, proto.Equal(
		&apiv1alpha1.ResourceReference{Namespace: "default", Name: "remote"},
		remoteCreate.GetRef(),
	))
	remote := &v1alpha3.RemoteMCPServer{}
	require.NoError(t, structuredobject.ToGo(remoteCreate.GetResource(), remoteMCPServerKind, remote, defaultGRPCMaxMessageSize))
	assert.Equal(t, "Remote server", remote.Spec.Description)
	assert.Equal(t, v1alpha3.RemoteMCPServerProtocolStreamableHttp, remote.Spec.Protocol)
	assert.Equal(t, "https://remote.example/mcp", remote.Spec.URL)
	assert.ElementsMatch(t, []v1alpha3.ValueRef{
		{Name: "Authorization", Value: "Bearer inline"},
		{Name: "X-Token", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "remote-token", Key: "token"}},
	}, remote.Spec.HeadersFrom)
	assert.Equal(t, 11*time.Second, remote.Spec.Timeout.Duration)
	assert.Equal(t, 12*time.Second, remote.Spec.SseReadTimeout.Duration)
	require.NotNil(t, remote.Spec.TerminateOnClose)
	assert.False(t, *remote.Spec.TerminateOnClose)

	managedCreate := service.createRequests[1]
	assert.Equal(t, managedMCPServerKind, managedCreate.GetType())
	managed := &kmcp.MCPServer{}
	require.NoError(t, structuredobject.ToGo(managedCreate.GetResource(), managedMCPServerKind, managed, defaultGRPCMaxMessageSize))
	assert.Equal(t, kmcp.TransportTypeStdio, managed.Spec.TransportType)
	assert.Equal(t, "npx", managed.Spec.Deployment.Cmd)
	assert.Equal(t, []string{"server-everything"}, managed.Spec.Deployment.Args)
	assert.Equal(t, map[string]string{"DIRECT": "value", "INLINE": "inline-value"}, managed.Spec.Deployment.Env)
	assert.Equal(t, 15*time.Second, managed.Spec.Timeout.Duration)

	assert.True(t, proto.Equal(&apiv1alpha1.DeleteToolServerRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "remote"},
	}, service.deleteRequest))
	require.Len(t, service.observations, 5)
	for _, observation := range service.observations {
		assert.Equal(t, "test-user", observation.userID)
		assert.True(t, observation.hasDeadline)
	}
	assert.Equal(t, int32(1), dialCount.Load())
}

func TestToolClientsValidateRequestsBeforeCallingServer(t *testing.T) {
	clientSet := New("http://unused.invalid", WithGRPCTarget(""), WithUserID("test-user"))
	t.Cleanup(func() { _ = clientSet.Close() })

	_, err := clientSet.ToolServer.CreateToolServer(t.Context(), nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = clientSet.ToolServer.CreateToolServer(t.Context(), &legacyv1alpha1.ToolServer{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	err = clientSet.ToolServer.DeleteToolServer(t.Context(), "", "name")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	invalidHeader := &legacyv1alpha1.ToolServer{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid"},
		Spec: legacyv1alpha1.ToolServerSpec{Config: legacyv1alpha1.ToolServerConfig{
			Type: legacyv1alpha1.ToolServerTypeSse,
			Sse: &legacyv1alpha1.SseMcpServerConfig{HttpToolServerConfig: legacyv1alpha1.HttpToolServerConfig{
				URL:     "https://example.com/sse",
				Headers: map[string]legacyv1alpha1.AnyType{"X-Count": {RawMessage: json.RawMessage(`1`)}},
			}},
		}},
	}
	_, err = clientSet.ToolServer.CreateToolServer(t.Context(), invalidHeader)
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
