package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const agentInstanceClientTestID = "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"

type recordingAgentInstanceService struct {
	apiv1alpha1.UnimplementedAgentInstanceServiceServer
	observation callObservation
}

func (s *recordingAgentInstanceService) CreateAgentInstance(ctx context.Context, _ *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.observation = callObservation{userID: first(values.Get(userIDHeader)), hasDeadline: hasDeadline}
	return &apiv1alpha1.CreateAgentInstanceResponse{}, nil
}

type a2aCallObservation struct {
	namespace     string
	id            string
	userID        string
	authorization string
	hasDeadline   bool
}

type recordingA2AService struct {
	a2apb.UnimplementedA2AServiceServer
	mu           sync.Mutex
	observations []a2aCallObservation
}

func (s *recordingA2AService) SendMessage(ctx context.Context, _ *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	s.observe(ctx)
	return pbconv.ToProtoSendMessageResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
}

func (s *recordingA2AService) SendStreamingMessage(_ *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	s.observe(stream.Context())
	response, err := pbconv.ToProtoStreamResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
	if err != nil {
		return err
	}
	return stream.Send(response)
}

func (s *recordingA2AService) SubscribeToTask(_ *a2apb.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	s.observe(stream.Context())
	response, err := pbconv.ToProtoStreamResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
	if err != nil {
		return err
	}
	return stream.Send(response)
}

func (s *recordingA2AService) observe(ctx context.Context) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, a2aCallObservation{
		namespace:     first(values.Get(kagenta2a.AgentInstanceNamespaceHeader)),
		id:            first(values.Get(kagenta2a.AgentInstanceIDHeader)),
		userID:        first(values.Get(userIDHeader)),
		authorization: first(values.Get("authorization")),
		hasDeadline:   hasDeadline,
	})
}

func TestAgentInstanceAndA2AClientsShareGRPCConnection(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	agentInstanceService := &recordingAgentInstanceService{}
	a2aService := &recordingA2AService{}
	server := grpc.NewServer()
	apiv1alpha1.RegisterAgentInstanceServiceServer(server, agentInstanceService)
	a2apb.RegisterA2AServiceServer(server, a2aService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("caller"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	_, err := clientSet.AgentInstance.CreateAgentInstance(context.Background(), &apiv1alpha1.CreateAgentInstanceRequest{})
	require.NoError(t, err)
	assert.Equal(t, callObservation{userID: "caller", hasDeadline: true}, agentInstanceService.observation)

	a2aClient, err := clientSet.A2A.ForAgentInstance(context.Background(), "kagent", agentInstanceClientTestID)
	require.NoError(t, err)
	a2aCtx := a2aclient.AttachServiceParams(context.Background(), a2aclient.ServiceParams{
		"authorization": {"Bearer model-key"},
	})
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}
	_, err = a2aClient.SendMessage(a2aCtx, request)
	require.NoError(t, err)
	for _, streamErr := range a2aClient.SendStreamingMessage(a2aCtx, request) {
		require.NoError(t, streamErr)
	}
	for _, streamErr := range a2aClient.SubscribeToTask(a2aCtx, &a2atype.SubscribeToTaskRequest{ID: "task-id"}) {
		require.NoError(t, streamErr)
	}

	a2aService.mu.Lock()
	require.Equal(t, []a2aCallObservation{
		{namespace: "kagent", id: agentInstanceClientTestID, userID: "caller", authorization: "Bearer model-key", hasDeadline: true},
		{namespace: "kagent", id: agentInstanceClientTestID, userID: "caller", authorization: "Bearer model-key", hasDeadline: false},
		{namespace: "kagent", id: agentInstanceClientTestID, userID: "caller", authorization: "Bearer model-key", hasDeadline: false},
	}, a2aService.observations)
	a2aService.mu.Unlock()
	assert.Equal(t, int32(1), dialCount.Load())
}

func TestStreamingA2AMethodsMatchUpstreamService(t *testing.T) {
	methods := make([]string, 0, len(a2apb.A2AService_ServiceDesc.Streams))
	for _, stream := range a2apb.A2AService_ServiceDesc.Streams {
		methods = append(methods, stream.StreamName)
		assert.True(t, isStreamingA2AMethod(stream.StreamName), "streaming method %q must not receive a unary timeout", stream.StreamName)
	}
	assert.Equal(t, []string{"SendStreamingMessage", "SubscribeToTask"}, methods)
}
