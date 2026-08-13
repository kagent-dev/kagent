package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	feedbackservice "github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type generatedClientFeedbackStore struct {
	database.Client
	feedback []database.Feedback
}

func (store *generatedClientFeedbackStore) StoreFeedback(_ context.Context, value *database.Feedback) error {
	createdAt := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	copy := *value
	copy.ID = 7
	copy.CreatedAt = &createdAt
	store.feedback = append(store.feedback, copy)
	return nil
}

func (store *generatedClientFeedbackStore) ListFeedback(_ context.Context, userID string) ([]database.Feedback, error) {
	result := make([]database.Feedback, 0, len(store.feedback))
	for _, value := range store.feedback {
		if value.UserID == userID {
			result = append(result, value)
		}
	}
	return result, nil
}

func TestSystemAndFeedbackGeneratedClients(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "Zoo"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
	).Build()
	store := &generatedClientFeedbackStore{}

	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:        listener,
		Registerer:      prometheus.NewRegistry(),
		Authenticator:   &authimpl.UnsecureAuthenticator{},
		SystemService:   systemservice.NewService(systemservice.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, nil)),
		FeedbackService: feedbackservice.NewService(store),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancelServer()
		if err := <-done; err != nil {
			t.Errorf("gRPC server shutdown error = %v", err)
		}
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	userContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "system-user"))
	systemClient := apiv1alpha1.NewSystemServiceClient(connection)
	currentUser, err := systemClient.GetCurrentUser(userContext, &apiv1alpha1.GetCurrentUserRequest{})
	if err != nil {
		t.Fatalf("GetCurrentUser() error = %v", err)
	}
	if got := currentUser.GetClaims().GetFields()["sub"].GetStringValue(); got != "system-user" {
		t.Fatalf("GetCurrentUser() sub = %q, want system-user", got)
	}

	namespaces, err := systemClient.ListNamespaces(userContext, &apiv1alpha1.ListNamespacesRequest{})
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if len(namespaces.GetNamespaces()) != 2 || namespaces.GetNamespaces()[0].GetName() != "alpha" || namespaces.GetNamespaces()[1].GetName() != "Zoo" {
		t.Fatalf("ListNamespaces() = %+v, want [alpha Zoo]", namespaces.GetNamespaces())
	}

	substrateStatus, err := systemClient.GetSubstrateStatus(userContext, &apiv1alpha1.GetSubstrateStatusRequest{Namespace: "alpha"})
	if err != nil {
		t.Fatalf("GetSubstrateStatus() error = %v", err)
	}
	if substrateStatus.GetEnabled() || len(substrateStatus.GetWorkerPools()) != 0 {
		t.Fatalf("GetSubstrateStatus() = %+v, want disabled empty inventory", substrateStatus)
	}

	feedbackClient := apiv1alpha1.NewFeedbackServiceClient(connection)
	messageID := int64(42)
	issueType := "factual"
	_, err = feedbackClient.CreateFeedback(userContext, &apiv1alpha1.CreateFeedbackRequest{
		MessageId:    &messageID,
		IsPositive:   false,
		FeedbackText: "incorrect answer",
		IssueType:    &issueType,
	})
	if err != nil {
		t.Fatalf("CreateFeedback() error = %v", err)
	}

	listed, err := feedbackClient.ListFeedback(userContext, &apiv1alpha1.ListFeedbackRequest{})
	if err != nil {
		t.Fatalf("ListFeedback() error = %v", err)
	}
	if len(listed.GetFeedback()) != 1 {
		t.Fatalf("ListFeedback() count = %d, want 1", len(listed.GetFeedback()))
	}
	gotFeedback := listed.GetFeedback()[0]
	if gotFeedback.GetId() != 7 || gotFeedback.GetUserId() != "system-user" || gotFeedback.GetMessageId() != messageID || gotFeedback.GetIssueType() != issueType {
		t.Fatalf("ListFeedback()[0] = %+v", gotFeedback)
	}
	if gotFeedback.GetCreatedAt().AsTime() != time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC) {
		t.Fatalf("ListFeedback()[0].created_at = %v", gotFeedback.GetCreatedAt())
	}

	otherUserContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "other-user"))
	otherFeedback, err := feedbackClient.ListFeedback(otherUserContext, &apiv1alpha1.ListFeedbackRequest{})
	if err != nil {
		t.Fatalf("ListFeedback(other user) error = %v", err)
	}
	if len(otherFeedback.GetFeedback()) != 0 {
		t.Fatalf("ListFeedback(other user) = %+v, want no records", otherFeedback.GetFeedback())
	}
}
