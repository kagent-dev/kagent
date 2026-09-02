package client

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type recordingAgentTemplateService struct {
	apiv1alpha1.UnimplementedAgentTemplateServiceServer

	mu           sync.Mutex
	observations []callObservation
	create       *apiv1alpha1.CreateAgentTemplateRequest
	update       *apiv1alpha1.UpdateAgentTemplateRequest
}

func (s *recordingAgentTemplateService) CreateAgentTemplate(ctx context.Context, request *apiv1alpha1.CreateAgentTemplateRequest) (*apiv1alpha1.CreateAgentTemplateResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.create = request
	return &apiv1alpha1.CreateAgentTemplateResponse{}, nil
}

func (s *recordingAgentTemplateService) UpdateAgentTemplate(ctx context.Context, request *apiv1alpha1.UpdateAgentTemplateRequest) (*apiv1alpha1.UpdateAgentTemplateResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.update = request
	return &apiv1alpha1.UpdateAgentTemplateResponse{}, nil
}

func (s *recordingAgentTemplateService) observe(ctx context.Context) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, callObservation{userID: first(values.Get(userIDHeader)), hasDeadline: hasDeadline})
}

func TestAgentTemplateClientUsesGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	service := &recordingAgentTemplateService{}
	server := grpc.NewServer()
	apiv1alpha1.RegisterAgentTemplateServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("caller"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	ref := &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "researcher"}
	_, err := clientSet.AgentTemplate.CreateAgentTemplate(t.Context(), &apiv1alpha1.CreateAgentTemplateRequest{Ref: ref})
	require.NoError(t, err)
	_, err = clientSet.AgentTemplate.UpdateAgentTemplate(t.Context(), &apiv1alpha1.UpdateAgentTemplateRequest{Ref: ref})
	require.NoError(t, err)
	service.mu.Lock()
	defer service.mu.Unlock()
	assert.True(t, proto.Equal(ref, service.create.GetRef()))
	assert.True(t, proto.Equal(ref, service.update.GetRef()))
	assert.Equal(t, []callObservation{{userID: "caller", hasDeadline: true}, {userID: "caller", hasDeadline: true}}, service.observations)
}
