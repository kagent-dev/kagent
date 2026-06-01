package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
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

func (r *recordingActorClient) deleteActor(_ context.Context, actorID string) (bool, error) {
	r.deleted = append(r.deleted, actorID)
	return true, nil
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
	p := &Lifecycle{Client: kube, deleteActor: rec.deleteActor}

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
