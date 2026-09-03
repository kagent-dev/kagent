package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type recordingSystemService struct {
	apiv1alpha1.UnimplementedSystemServiceServer

	mu           sync.Mutex
	observations []callObservation
}

func (service *recordingSystemService) GetVersion(ctx context.Context, _ *apiv1alpha1.GetVersionRequest) (*apiv1alpha1.GetVersionResponse, error) {
	service.observe(ctx)
	return &apiv1alpha1.GetVersionResponse{
		KagentVersion: "v1.2.3",
		GitCommit:     "abc123",
		BuildDate:     "2026-07-29",
	}, nil
}

func (service *recordingSystemService) ListNamespaces(ctx context.Context, _ *apiv1alpha1.ListNamespacesRequest) (*apiv1alpha1.ListNamespacesResponse, error) {
	service.observe(ctx)
	return &apiv1alpha1.ListNamespacesResponse{Namespaces: []*apiv1alpha1.Namespace{
		{Name: "alpha", Status: "Active"},
		{Name: "team", Status: "Terminating"},
	}}, nil
}

func (service *recordingSystemService) observe(ctx context.Context) {
	metadataValues, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.observations = append(service.observations, callObservation{
		userID:      first(metadataValues.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

func TestSystemClientsUseGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	systemService := &recordingSystemService{}
	server := grpc.NewServer()
	apiv1alpha1.RegisterSystemServiceServer(server, systemService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("default-user"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	version, err := clientSet.Version.GetVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, &api.VersionResponse{
		KAgentVersion: "v1.2.3",
		GitCommit:     "abc123",
		BuildDate:     "2026-07-29",
	}, version)

	namespaces, err := clientSet.Namespace.ListNamespaces(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Successfully listed namespaces", namespaces.Message)
	assert.Equal(t, []api.NamespaceResponse{
		{Name: "alpha", Status: "Active"},
		{Name: "team", Status: "Terminating"},
	}, namespaces.Data)

	systemService.mu.Lock()
	require.Len(t, systemService.observations, 2)
	for _, observation := range systemService.observations {
		assert.Equal(t, callObservation{userID: "default-user", hasDeadline: true}, observation)
	}
	systemService.mu.Unlock()
	assert.Equal(t, int32(1), dialCount.Load())
}
