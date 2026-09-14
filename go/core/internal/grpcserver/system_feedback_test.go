package grpcserver

import (
	"context"
	"net"
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	if err := atev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("atev1alpha1.AddToScheme() error = %v", err)
	}
	workerPool := &atev1alpha1.WorkerPool{
		ObjectMeta: metav1.ObjectMeta{Namespace: "alpha", Name: "pool", Labels: map[string]string{"team": "agents"}},
		Spec: atev1alpha1.WorkerPoolSpec{
			Replicas: 2, WorkerImage: "ateom:test",
			Template: &atev1alpha1.WorkerPoolPodTemplate{NodeSelector: map[string]string{"disk": "ssd"}},
		},
		Status: atev1alpha1.WorkerPoolStatus{Replicas: 2, ReadyReplicas: 1},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		workerPool,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "Zoo"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
	).Build()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &authimpl.UnsecureAuthenticator{},
		SystemService: systemservice.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, emptySystemATEClient{}),
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

	/*
	 * The three paged reads, over the wire rather than against the service directly.
	 *
	 * What only this level can say: that each one is in the method-policy map, that the
	 * shared PageRequest/PageResponse survives the round trip, and that an empty inventory
	 * is a successful answer. A service-level test sees none
	 * of that — it never passes through the interceptors or the generated client.
	 */
	summary, err := systemClient.GetSubstrateSummary(userContext, &apiv1alpha1.GetSubstrateSummaryRequest{Namespace: "alpha"})
	if err != nil {
		t.Fatalf("GetSubstrateSummary() error = %v", err)
	}
	if !summary.GetEnabled() || summary.GetActorCount() != 0 || len(summary.GetWorkerPools()) != 1 {
		t.Fatalf("GetSubstrateSummary() = %+v, want enabled summary with one worker pool", summary)
	}

	for _, pool := range summary.GetWorkerPools() {
		assert.Equal(t, "alpha", pool.GetRef().GetNamespace())
		assert.Equal(t, "pool", pool.GetRef().GetName())
		assert.Equal(t, atev1alpha1.GroupVersion.String(), pool.GetResource().GetApiVersion())
		var decoded atev1alpha1.WorkerPool
		require.NoError(t, structuredobject.ToGo(pool.GetResource(), "WorkerPool", &decoded, DefaultMaxMessageSize))
		assert.Equal(t, workerPool.Labels, decoded.Labels)
		assert.Equal(t, workerPool.Name, decoded.Name)
		assert.Equal(t, workerPool.Namespace, decoded.Namespace)
		assert.Equal(t, workerPool.Spec, decoded.Spec)
		assert.Equal(t, workerPool.Status, decoded.Status)
	}

	actors, err := systemClient.ListSubstrateActors(userContext, &apiv1alpha1.ListSubstrateActorsRequest{
		Atespace: "alpha",
		Page:     &apiv1alpha1.PageRequest{Limit: 100},
	})
	if err != nil {
		t.Fatalf("ListSubstrateActors() error = %v", err)
	}
	if !actors.GetEnabled() || len(actors.GetActors()) != 0 || actors.GetPage().GetNextPageToken() != "" {
		t.Fatalf("ListSubstrateActors() = %+v, want enabled empty page", actors)
	}

	workers, err := systemClient.ListSubstrateWorkers(userContext, &apiv1alpha1.ListSubstrateWorkersRequest{
		Namespace: "alpha",
		Page:      &apiv1alpha1.PageRequest{Limit: 100},
	})
	if err != nil {
		t.Fatalf("ListSubstrateWorkers() error = %v", err)
	}
	if !workers.GetEnabled() || len(workers.GetWorkers()) != 0 || workers.GetPage().GetNextPageToken() != "" {
		t.Fatalf("ListSubstrateWorkers() = %+v, want enabled empty page", workers)
	}

	for _, namespace := range []string{"", "a", "team-1", strings.Repeat("a", 63), "INVALID_NAMESPACE", "-team", "team-", "team.name", " team", strings.Repeat("a", 64)} {
		t.Run("namespace/"+namespace, func(t *testing.T) {
			want := codes.InvalidArgument
			if namespace == "" || namespace == "a" || namespace == "team-1" || namespace == strings.Repeat("a", 63) {
				want = codes.OK
			}
			_, err := systemClient.GetSubstrateSummary(userContext, &apiv1alpha1.GetSubstrateSummaryRequest{Namespace: namespace})
			assert.Equal(t, want, status.Code(err), "summary")
			_, err = systemClient.ListSubstrateActors(userContext, &apiv1alpha1.ListSubstrateActorsRequest{Atespace: namespace})
			assert.Equal(t, want, status.Code(err), "actors")
			_, err = systemClient.GetSubstrateSummary(userContext, &apiv1alpha1.GetSubstrateSummaryRequest{Atespace: namespace})
			assert.Equal(t, want, status.Code(err), "summary atespace")
			_, err = systemClient.ListSubstrateWorkers(userContext, &apiv1alpha1.ListSubstrateWorkersRequest{Namespace: namespace})
			assert.Equal(t, want, status.Code(err), "workers")
		})
	}

	for _, tc := range []struct {
		name        string
		limit       int32
		filter      string
		actorField  apiv1alpha1.SubstrateActorSortField
		workerField apiv1alpha1.SubstrateWorkerSortField
		order       apiv1alpha1.SubstrateSortOrder
	}{
		{name: "negative limit", limit: -1},
		{name: "oversized limit", limit: 101},
		{name: "oversized filter", filter: strings.Repeat("a", 201)},
		{name: "unknown field", actorField: 999, workerField: 999},
		{name: "unknown order", order: 999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := systemClient.ListSubstrateActors(userContext, &apiv1alpha1.ListSubstrateActorsRequest{
				Page: &apiv1alpha1.PageRequest{Limit: tc.limit}, Filter: tc.filter, SortField: tc.actorField, SortOrder: tc.order,
			})
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "actors")
			_, err = systemClient.ListSubstrateWorkers(userContext, &apiv1alpha1.ListSubstrateWorkersRequest{
				Page: &apiv1alpha1.PageRequest{Limit: tc.limit}, Filter: tc.filter, SortField: tc.workerField, SortOrder: tc.order,
			})
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "workers")
		})
	}
}

// Empty upstream inventories still exercise each service read through the RPCs.
type emptySystemATEClient struct{}

var _ systemservice.ATEClient = emptySystemATEClient{}

func (emptySystemATEClient) ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error) {
	return nil, nil
}

func (emptySystemATEClient) ListActorsPage(context.Context, string, int32, string) ([]*ateapipb.Actor, string, error) {
	return nil, "", nil
}

func (emptySystemATEClient) ListWorkersPage(context.Context, int32, string) ([]*ateapipb.Worker, string, error) {
	return nil, "", nil
}
