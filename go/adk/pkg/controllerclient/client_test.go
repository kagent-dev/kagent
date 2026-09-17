package controllerclient

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type mutableTokenProvider struct {
	mu    sync.RWMutex
	token string
}

func (provider *mutableTokenProvider) GetToken() string {
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	return provider.token
}

func (provider *mutableTokenProvider) set(token string) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.token = token
}

type metadataMemoryServer struct {
	apiv1alpha1.UnimplementedMemoryServiceServer
	metadata  []metadata.MD
	deadlines []bool
}

func (server *metadataMemoryServer) List(ctx context.Context, _ *apiv1alpha1.MemoryServiceListRequest) (*apiv1alpha1.MemoryServiceListResponse, error) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	server.metadata = append(server.metadata, values)
	server.deadlines = append(server.deadlines, hasDeadline)
	return &apiv1alpha1.MemoryServiceListResponse{}, nil
}

func TestClientAddsDynamicMetadataAndDeadlines(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	service := &metadataMemoryServer{}
	grpcServer := grpc.NewServer()
	apiv1alpha1.RegisterMemoryServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	tokens := &mutableTokenProvider{token: "first-token"}
	client, err := New(Config{
		APIURL:        "http://bufnet:80",
		AgentName:     "default/agent",
		TokenProvider: tokens,
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	ctx, cancel := client.CallContext(auth.WithUserID(t.Context(), "context-user"), "")
	_, err = client.MemoryService().List(ctx, &apiv1alpha1.MemoryServiceListRequest{})
	cancel()
	require.NoError(t, err)

	tokens.set("second-token")
	ctx, cancel = client.CallContext(t.Context(), "explicit-user")
	_, err = client.MemoryService().List(ctx, &apiv1alpha1.MemoryServiceListRequest{})
	cancel()
	require.NoError(t, err)

	require.Len(t, service.metadata, 2)
	assert.Equal(t, []string{"Bearer first-token"}, service.metadata[0].Get("authorization"))
	assert.Equal(t, []string{"context-user"}, service.metadata[0].Get("x-user-id"))
	assert.Equal(t, []string{"default/agent"}, service.metadata[0].Get("x-agent-name"))
	assert.Equal(t, []string{"Bearer second-token"}, service.metadata[1].Get("authorization"))
	assert.Equal(t, []string{"explicit-user"}, service.metadata[1].Get("x-user-id"))
	assert.Equal(t, []bool{true, true}, service.deadlines)
}

func TestClientRequiresAPIURL(t *testing.T) {
	_, err := New(Config{})
	require.EqualError(t, err, "controller API URL \"\" must contain only a scheme and authority")
}

func TestTargetFromURL(t *testing.T) {
	target, secure, err := targetFromURL("https://api.example.com:8443")
	require.NoError(t, err)
	assert.Equal(t, "passthrough:///api.example.com:8443", target)
	assert.True(t, secure)
}

func TestClientCanDisableDefaultDeadline(t *testing.T) {
	client := &Client{timeout: -time.Second}
	ctx, cancel := client.CallContext(t.Context(), "")
	defer cancel()
	_, hasDeadline := ctx.Deadline()
	assert.False(t, hasDeadline)
}
