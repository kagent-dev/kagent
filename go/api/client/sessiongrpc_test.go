package client

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const sessionClientTestID = "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"

type recordingSessionService struct {
	apiv1alpha1.UnimplementedSessionServiceServer
	observation callObservation
}

func (s *recordingSessionService) CreateSession(ctx context.Context, _ *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
	s.observation = observeCall(ctx)
	return &apiv1alpha1.CreateSessionResponse{}, nil
}

func (s *recordingSessionService) GetSession(ctx context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
	s.observation = observeCall(ctx)
	return &apiv1alpha1.GetSessionResponse{Session: &apiv1alpha1.Session{
		Id: request.SessionId, Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}}, nil
}

func observeCall(ctx context.Context) callObservation {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	return callObservation{
		userID:        first(values.Get(userIDHeader)),
		authorization: strings.Join(values.Get(authorizationHeader), ","),
		hasDeadline:   hasDeadline,
	}
}

type a2aCallObservation struct {
	contextID     string
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

func (s *recordingA2AService) SendMessage(ctx context.Context, req *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	s.observe(ctx, req.Tenant, req.Message.GetContextId())
	return pbconv.ToProtoSendMessageResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
}

func (s *recordingA2AService) SendStreamingMessage(req *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	s.observe(stream.Context(), req.Tenant, "")
	response, err := pbconv.ToProtoStreamResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
	if err != nil {
		return err
	}
	return stream.Send(response)
}

func (s *recordingA2AService) SubscribeToTask(req *a2apb.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	s.observe(stream.Context(), req.Tenant, "")
	response, err := pbconv.ToProtoStreamResponse(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")))
	if err != nil {
		return err
	}
	return stream.Send(response)
}

func (s *recordingA2AService) ListTaskPushNotificationConfigs(ctx context.Context, req *a2apb.ListTaskPushNotificationConfigsRequest) (*a2apb.ListTaskPushNotificationConfigsResponse, error) {
	s.observe(ctx, req.Tenant, "")
	return &a2apb.ListTaskPushNotificationConfigsResponse{
		Configs:       []*a2apb.TaskPushNotificationConfig{{Id: "callback", TaskId: req.TaskId, Url: "https://receiver.example/callback"}},
		NextPageToken: "next",
	}, nil
}

func (s *recordingA2AService) CreateTaskPushNotificationConfig(ctx context.Context, req *a2apb.TaskPushNotificationConfig) (*a2apb.TaskPushNotificationConfig, error) {
	s.observe(ctx, req.Tenant, "")
	return req, nil
}

func (s *recordingA2AService) observe(ctx context.Context, tenant, contextID string) {
	values, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, a2aCallObservation{
		id:            tenant,
		contextID:     contextID,
		userID:        first(values.Get(userIDHeader)),
		authorization: strings.Join(values.Get(authorizationHeader), ","),
		hasDeadline:   hasDeadline,
	})
}

// serveRecordingServices runs both recording services on one in-process
// listener and returns the dialer that reaches it, counting the dials.
func serveRecordingServices(t *testing.T, sessionService *recordingSessionService, a2aService *recordingA2AService) (grpc.DialOption, *atomic.Int32) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterSessionServiceServer(server, sessionService)
	a2apb.RegisterA2AServiceServer(server, a2aService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialCount := &atomic.Int32{}
	return grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		dialCount.Add(1)
		return listener.Dial()
	}), dialCount
}

func TestSessionAndA2AClientsUseTheirEndpoints(t *testing.T) {
	sessionService := &recordingSessionService{}
	a2aService := &recordingA2AService{}
	dialer, dialCount := serveRecordingServices(t, sessionService, a2aService)

	options := []ClientOption{
		WithUserID("caller"),
		WithGRPCTimeout(5 * time.Second),
		WithGRPCDialOptions(dialer),
	}
	apiClient, err := NewAPI(
		"http://api.invalid:80",
		options...,
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, apiClient.Close()) })
	gatewayClient, err := NewGateway("http://gateway.invalid:80", options...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gatewayClient.Close()) })

	_, err = apiClient.Session.CreateSession(context.Background(), &apiv1alpha1.CreateSessionRequest{})
	require.NoError(t, err)
	assert.Equal(t, callObservation{userID: "caller", hasDeadline: true}, sessionService.observation)

	a2aClient, err := gatewayClient.A2A.ForAgent(context.Background(), &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"})
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
		{id: "team-a/assistant", userID: "caller", authorization: "Bearer model-key", hasDeadline: true},
		{id: "team-a/assistant", userID: "caller", authorization: "Bearer model-key", hasDeadline: false},
		{id: "team-a/assistant", userID: "caller", authorization: "Bearer model-key", hasDeadline: false},
	}, a2aService.observations)
	a2aService.mu.Unlock()
	sessionClient, err := gatewayClient.A2A.ForSession(context.Background(), sessionClientTestID)
	require.NoError(t, err)
	_, err = sessionClient.SendMessage(context.Background(), &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser)})
	require.NoError(t, err)
	a2aService.mu.Lock()
	require.Equal(t, sessionClientTestID, a2aService.observations[3].contextID)
	require.Equal(t, "team-a/assistant", a2aService.observations[3].id)
	a2aService.mu.Unlock()
	created, err := sessionClient.CreateTaskPushConfig(context.Background(), &a2atype.PushConfig{
		TaskID: "task-id", ID: "callback", URL: "https://receiver.example/callback",
	})
	require.NoError(t, err)
	require.Equal(t, "callback", created.ID)
	a2aService.mu.Lock()
	require.Equal(t, a2aCallObservation{id: "team-a/assistant", userID: "caller", hasDeadline: true}, a2aService.observations[4])
	a2aService.mu.Unlock()
	page, err := gatewayClient.A2A.ListTaskPushConfigsPage(context.Background(), sessionClientTestID, "task-id", 1, "previous")
	require.NoError(t, err)
	require.Equal(t, "next", page.GetNextPageToken())
	require.Equal(t, "callback", page.GetConfigs()[0].GetId())
	a2aService.mu.Lock()
	require.Equal(t, a2aCallObservation{id: "team-a/assistant", userID: "caller", hasDeadline: true}, a2aService.observations[5])
	a2aService.mu.Unlock()
	assert.Equal(t, int32(2), dialCount.Load())
}

func TestBearerTokenAuthenticatesEveryCall(t *testing.T) {
	sessionService := &recordingSessionService{}
	a2aService := &recordingA2AService{}
	dialer, _ := serveRecordingServices(t, sessionService, a2aService)

	options := []ClientOption{
		WithUserID("caller"),
		WithBearerToken("caller-jwt"),
		WithGRPCTimeout(5 * time.Second),
		WithGRPCDialOptions(dialer),
	}
	apiClient, err := NewAPI("http://api.invalid:80", options...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, apiClient.Close()) })
	gatewayClient, err := NewGateway("http://gateway.invalid:80", options...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gatewayClient.Close()) })

	// Control plane: the bearer rides beside the user ID on every call.
	_, err = apiClient.Session.CreateSession(context.Background(), &apiv1alpha1.CreateSessionRequest{})
	require.NoError(t, err)
	assert.Equal(t, callObservation{userID: "caller", authorization: "Bearer caller-jwt", hasDeadline: true}, sessionService.observation)

	// Agent traffic: the Session lookup and the A2A calls carry it too, and a
	// bearer attached to the context (a model key passed through) keeps the
	// slot to itself, since the server reads one value.
	a2aClient, err := gatewayClient.A2A.ForSession(context.Background(), sessionClientTestID)
	require.NoError(t, err)
	assert.Equal(t, "Bearer caller-jwt", sessionService.observation.authorization)

	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}
	_, err = a2aClient.SendMessage(context.Background(), request)
	require.NoError(t, err)
	for _, streamErr := range a2aClient.SendStreamingMessage(context.Background(), request) {
		require.NoError(t, streamErr)
	}
	modelKeyCtx := a2aclient.AttachServiceParams(context.Background(), a2aclient.ServiceParams{
		authorizationHeader: {"Bearer model-key"},
	})
	_, err = a2aClient.SendMessage(modelKeyCtx, request)
	require.NoError(t, err)

	a2aService.mu.Lock()
	defer a2aService.mu.Unlock()
	require.Len(t, a2aService.observations, 3)
	assert.Equal(t, "Bearer caller-jwt", a2aService.observations[0].authorization)
	assert.Equal(t, "Bearer caller-jwt", a2aService.observations[1].authorization)
	assert.Equal(t, "Bearer model-key", a2aService.observations[2].authorization)
	for _, observation := range a2aService.observations {
		assert.Equal(t, "caller", observation.userID)
	}
}

func TestStreamingA2AMethodsMatchUpstreamService(t *testing.T) {
	methods := make([]string, 0, len(a2apb.A2AService_ServiceDesc.Streams))
	for _, stream := range a2apb.A2AService_ServiceDesc.Streams {
		methods = append(methods, stream.StreamName)
		assert.True(t, isStreamingA2AMethod(stream.StreamName), "streaming method %q must not receive a unary timeout", stream.StreamName)
	}
	assert.Equal(t, []string{"SendStreamingMessage", "SubscribeToTask"}, methods)
}
