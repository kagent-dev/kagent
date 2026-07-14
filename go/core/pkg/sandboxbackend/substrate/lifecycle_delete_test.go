package substrate

import (
	"context"
	"slices"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingActorClient struct {
	deleted []string
}

func (r *recordingActorClient) GetActor(_ context.Context, in *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.GetActorResponse, error) {
	name := in.GetActorRef().GetName()
	if slices.Contains(r.deleted, name) {
		return nil, status.Error(codes.NotFound, "actor deleted")
	}
	return &ateapipb.GetActorResponse{
		Actor: &ateapipb.Actor{
			ActorId:  name,
			Atespace: in.GetActorRef().GetAtespace(),
			Status:   ateapipb.Actor_STATUS_SUSPENDED,
		},
	}, nil
}

func (r *recordingActorClient) DeleteActor(_ context.Context, in *ateapipb.DeleteActorRequest, _ ...grpc.CallOption) (*ateapipb.DeleteActorResponse, error) {
	r.deleted = append(r.deleted, in.GetActorRef().GetName())
	return &ateapipb.DeleteActorResponse{}, nil
}

func (r *recordingActorClient) CreateActor(context.Context, *ateapipb.CreateActorRequest, ...grpc.CallOption) (*ateapipb.CreateActorResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) SuspendActor(context.Context, *ateapipb.SuspendActorRequest, ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) ResumeActor(context.Context, *ateapipb.ResumeActorRequest, ...grpc.CallOption) (*ateapipb.ResumeActorResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) PauseActor(context.Context, *ateapipb.PauseActorRequest, ...grpc.CallOption) (*ateapipb.PauseActorResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) UpdateActor(context.Context, *ateapipb.UpdateActorRequest, ...grpc.CallOption) (*ateapipb.UpdateActorResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) ListWorkers(context.Context, *ateapipb.ListWorkersRequest, ...grpc.CallOption) (*ateapipb.ListWorkersResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) ListActors(context.Context, *ateapipb.ListActorsRequest, ...grpc.CallOption) (*ateapipb.ListActorsResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) DebugClear(context.Context, *ateapipb.DebugClearRequest, ...grpc.CallOption) (*ateapipb.DebugClearResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) CreateAtespace(_ context.Context, in *ateapipb.CreateAtespaceRequest, _ ...grpc.CallOption) (*ateapipb.CreateAtespaceResponse, error) {
	return &ateapipb.CreateAtespaceResponse{Atespace: &ateapipb.Atespace{Name: in.GetName()}}, nil
}

func (r *recordingActorClient) GetAtespace(context.Context, *ateapipb.GetAtespaceRequest, ...grpc.CallOption) (*ateapipb.GetAtespaceResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) ListAtespaces(context.Context, *ateapipb.ListAtespacesRequest, ...grpc.CallOption) (*ateapipb.ListAtespacesResponse, error) {
	panic("not used")
}

func (r *recordingActorClient) DeleteAtespace(context.Context, *ateapipb.DeleteAtespaceRequest, ...grpc.CallOption) (*ateapipb.DeleteAtespaceResponse, error) {
	panic("not used")
}

func TestLifecycleCleanupGeneratedTemplate_DeletesGoldenActor(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	ns := "kagent"
	tmpl := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "peterj-claw", Namespace: ns, Labels: map[string]string{
			HarnessLabelKey: "peterj-claw",
		}},
		Status: atev1alpha1.ActorTemplateStatus{
			GoldenActorID: "golden-actor-uuid",
			Phase:         atev1alpha1.PhaseReady,
		},
	}
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "peterj-claw",
			Namespace: ns,
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()
	rec := &recordingActorClient{}
	p := &Lifecycle{Client: kube, AteClient: &Client{ControlClient: rec}}

	var complete bool
	var err error
	for range 5 {
		complete, err = p.CleanupGeneratedTemplate(context.Background(), ah)
		require.NoError(t, err)
		if complete {
			break
		}
	}
	require.True(t, complete, "CleanupGeneratedTemplate should finish within a few reconcile passes")
	require.Equal(t, []string{"golden-actor-uuid"}, rec.deleted)

	var got atev1alpha1.ActorTemplate
	require.NoError(t, kube.Get(context.Background(), client.ObjectKeyFromObject(tmpl), &got))
}
