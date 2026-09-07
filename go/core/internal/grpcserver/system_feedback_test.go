package grpcserver

import (
	"context"
	"net"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
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

func TestSystemGeneratedClient(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "Zoo"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
	).Build()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &authimpl.UnsecureAuthenticator{},
		SystemService: systemservice.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, nil, nil),
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
}
