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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

	/*
	 * The three paged reads, over the wire rather than against the service directly.
	 *
	 * What only this level can say: that each one is in the method-policy map, that the
	 * shared PageRequest/PageResponse survives the round trip, and that an unconfigured
	 * substrate is an empty answer rather than an error. A service-level test sees none
	 * of that — it never passes through the interceptors or the generated client.
	 */
	summary, err := systemClient.GetSubstrateSummary(userContext, &apiv1alpha1.GetSubstrateSummaryRequest{Namespace: "alpha"})
	if err != nil {
		t.Fatalf("GetSubstrateSummary() error = %v", err)
	}
	if summary.GetEnabled() || summary.GetActorCount() != 0 || len(summary.GetWorkerPools()) != 0 {
		t.Fatalf("GetSubstrateSummary() = %+v, want disabled empty summary", summary)
	}

	actors, err := systemClient.ListSubstrateActors(userContext, &apiv1alpha1.ListSubstrateActorsRequest{
		Namespace: "alpha",
		Page:      &apiv1alpha1.PageRequest{Limit: 100},
	})
	if err != nil {
		t.Fatalf("ListSubstrateActors() error = %v", err)
	}
	if actors.GetEnabled() || len(actors.GetActors()) != 0 || actors.GetPage().GetNextPageToken() != "" {
		t.Fatalf("ListSubstrateActors() = %+v, want disabled empty page", actors)
	}

	workers, err := systemClient.ListSubstrateWorkers(userContext, &apiv1alpha1.ListSubstrateWorkersRequest{
		Namespace: "alpha",
		Page:      &apiv1alpha1.PageRequest{Limit: 100},
	})
	if err != nil {
		t.Fatalf("ListSubstrateWorkers() error = %v", err)
	}
	if workers.GetEnabled() || len(workers.GetWorkers()) != 0 || workers.GetPage().GetNextPageToken() != "" {
		t.Fatalf("ListSubstrateWorkers() = %+v, want disabled empty page", workers)
	}

	// The cap lives on PageRequest.limit rather than on these requests, so this is what
	// says the shared rule still reaches them: the interceptor refuses before a handler
	// runs, and the service's own guard never gets the chance.
	if _, err := systemClient.ListSubstrateActors(userContext, &apiv1alpha1.ListSubstrateActorsRequest{
		Namespace: "alpha",
		Page:      &apiv1alpha1.PageRequest{Limit: 101},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListSubstrateActors(limit 101) code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}
