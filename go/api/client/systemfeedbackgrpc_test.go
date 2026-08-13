package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

type recordingFeedbackService struct {
	apiv1alpha1.UnimplementedFeedbackServiceServer

	mu            sync.Mutex
	observations  []callObservation
	createRequest *apiv1alpha1.CreateFeedbackRequest
}

func (service *recordingFeedbackService) CreateFeedback(ctx context.Context, request *apiv1alpha1.CreateFeedbackRequest) (*apiv1alpha1.CreateFeedbackResponse, error) {
	service.observe(ctx)
	service.mu.Lock()
	service.createRequest = request
	service.mu.Unlock()
	return &apiv1alpha1.CreateFeedbackResponse{}, nil
}

func (service *recordingFeedbackService) ListFeedback(ctx context.Context, _ *apiv1alpha1.ListFeedbackRequest) (*apiv1alpha1.ListFeedbackResponse, error) {
	service.observe(ctx)
	messageID := int64(42)
	issueType := "factual"
	createdAt := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	return &apiv1alpha1.ListFeedbackResponse{Feedback: []*apiv1alpha1.Feedback{{
		Id:           7,
		CreatedAt:    timestamppb.New(createdAt),
		UserId:       "explicit-user",
		MessageId:    &messageID,
		IsPositive:   false,
		FeedbackText: "incorrect answer",
		IssueType:    &issueType,
	}}}, nil
}

func (service *recordingFeedbackService) observe(ctx context.Context) {
	metadataValues, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.observations = append(service.observations, callObservation{
		userID:      first(metadataValues.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

func TestSystemAndFeedbackClientsUseGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	systemService := &recordingSystemService{}
	feedbackService := &recordingFeedbackService{}
	server := grpc.NewServer()
	apiv1alpha1.RegisterSystemServiceServer(server, systemService)
	apiv1alpha1.RegisterFeedbackServiceServer(server, feedbackService)
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

	messageID := int64(42)
	issueType := database.FeedbackIssueTypeFactual
	feedback := &api.Feedback{
		MessageID:    &messageID,
		IsPositive:   false,
		FeedbackText: "incorrect answer",
		IssueType:    &issueType,
	}
	require.NoError(t, clientSet.Feedback.CreateFeedback(t.Context(), feedback, "explicit-user"))
	assert.Equal(t, "explicit-user", feedback.UserID)

	listed, err := clientSet.Feedback.ListFeedback(t.Context(), "explicit-user")
	require.NoError(t, err)
	assert.Equal(t, "Successfully listed feedback", listed.Message)
	require.Len(t, listed.Data, 1)
	assert.Equal(t, int64(7), listed.Data[0].ID)
	assert.Equal(t, messageID, *listed.Data[0].MessageID)
	assert.Equal(t, issueType, *listed.Data[0].IssueType)
	assert.Equal(t, time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC), *listed.Data[0].CreatedAt)

	systemService.mu.Lock()
	require.Len(t, systemService.observations, 2)
	for _, observation := range systemService.observations {
		assert.Equal(t, callObservation{userID: "default-user", hasDeadline: true}, observation)
	}
	systemService.mu.Unlock()

	feedbackService.mu.Lock()
	require.Len(t, feedbackService.observations, 2)
	for _, observation := range feedbackService.observations {
		assert.Equal(t, "explicit-user", observation.userID)
		assert.True(t, observation.hasDeadline)
	}
	require.NotNil(t, feedbackService.createRequest)
	assert.Equal(t, messageID, feedbackService.createRequest.GetMessageId())
	assert.Equal(t, "factual", feedbackService.createRequest.GetIssueType())
	assert.Equal(t, "incorrect answer", feedbackService.createRequest.GetFeedbackText())
	feedbackService.mu.Unlock()

	assert.Equal(t, int32(1), dialCount.Load())
}
