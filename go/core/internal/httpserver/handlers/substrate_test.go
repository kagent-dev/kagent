package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// noMatchKubeClient is a minimal client.Client stub whose List always returns
// a *meta.NoKindMatchError, simulating a cluster where the ate.dev CRDs are absent.
type noMatchKubeClient struct {
	client.Client
}

func (noMatchKubeClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return &apimeta.NoKindMatchError{}
}

// TestHandleGetSubstrateStatus_SubstrateNotConfigured verifies that when AteClient is nil
// (substrate not configured), the endpoint returns 200 with Enabled:false and empty slices
// without making any CRD List calls.
func TestHandleGetSubstrateStatus_SubstrateNotConfigured(t *testing.T) {
	t.Parallel()

	base := &handlers.Base{KubeClient: noMatchKubeClient{}, Authorizer: &auth.NoopAuthorizer{}}
	h := handlers.NewSubstrateHandler(base, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/substrate/status?namespace=kagent", nil)
	req = setUser(req, "test-user")
	rec := httptest.NewRecorder()
	h.HandleGetSubstrateStatus(&testErrorResponseWriter{ResponseWriter: rec}, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var wrapped api.StandardResponse[api.SubstrateStatusResponse]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wrapped))
	require.False(t, wrapped.Data.Enabled)
	require.Empty(t, wrapped.Data.WorkerPools)
	require.Empty(t, wrapped.Data.ActorTemplates)
	require.Empty(t, wrapped.Data.Actors)
	require.Empty(t, wrapped.Data.Workers)
}

type stubAteControl struct {
	ateapipb.ControlClient
	actors  []*ateapipb.Actor
	workers []*ateapipb.Worker
}

func (s *stubAteControl) ListActors(context.Context, *ateapipb.ListActorsRequest, ...grpc.CallOption) (*ateapipb.ListActorsResponse, error) {
	return &ateapipb.ListActorsResponse{Actors: s.actors}, nil
}

func (s *stubAteControl) ListWorkers(context.Context, *ateapipb.ListWorkersRequest, ...grpc.CallOption) (*ateapipb.ListWorkersResponse, error) {
	return &ateapipb.ListWorkersResponse{Workers: s.workers}, nil
}

func TestHandleGetSubstrateStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1alpha1.WorkerPool{
			ObjectMeta: metav1.ObjectMeta{Name: "default-wp", Namespace: "kagent"},
			Spec:       atev1alpha1.WorkerPoolSpec{Replicas: 2, AteomImage: "localhost:5001/ateom:latest"},
		},
		&atev1alpha1.ActorTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-claw",
				Namespace: "kagent",
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kagent",
					substrate.HarnessLabelKey:      "my-claw",
				},
			},
			Spec: atev1alpha1.ActorTemplateSpec{
				WorkerPoolRef: corev1.ObjectReference{Name: "default-wp", Namespace: "kagent"},
			},
			Status: atev1alpha1.ActorTemplateStatus{Phase: atev1alpha1.PhaseReady, GoldenActorID: "golden-1"},
		},
	).Build()

	ate := &substrate.Client{ControlClient: &stubAteControl{
		actors: []*ateapipb.Actor{{
			ActorId:                "ahr-kagent-my-claw",
			Status:                 ateapipb.Actor_STATUS_RUNNING,
			ActorTemplateNamespace: "kagent",
			ActorTemplateName:      "my-claw",
		}},
		workers: []*ateapipb.Worker{{
			WorkerNamespace: "kagent",
			WorkerPool:      "default-wp",
			WorkerPod:       "ateom-0",
			ActorId:         "ahr-kagent-my-claw",
		}},
	}}

	base := &handlers.Base{KubeClient: kube, Authorizer: &auth.NoopAuthorizer{}}
	h := handlers.NewSubstrateHandler(base, ate)

	req := httptest.NewRequest(http.MethodGet, "/api/substrate/status?namespace=kagent", nil)
	req = setUser(req, "test-user")
	rec := httptest.NewRecorder()
	h.HandleGetSubstrateStatus(&testErrorResponseWriter{ResponseWriter: rec}, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var wrapped api.StandardResponse[api.SubstrateStatusResponse]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wrapped))
	require.True(t, wrapped.Data.Enabled)
	require.Len(t, wrapped.Data.WorkerPools, 1)
	require.Equal(t, "default-wp", wrapped.Data.WorkerPools[0].Name)
	require.Len(t, wrapped.Data.ActorTemplates, 1)
	require.Equal(t, "Ready", wrapped.Data.ActorTemplates[0].Phase)
	require.True(t, wrapped.Data.ActorTemplates[0].ManagedByKagent)
	require.Equal(t, "my-claw", wrapped.Data.ActorTemplates[0].HarnessName)
	require.Len(t, wrapped.Data.Actors, 1)
	require.Equal(t, "Running", wrapped.Data.Actors[0].Status)
	require.Len(t, wrapped.Data.Workers, 1)
}
