package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingActorDeleter struct {
	deleted []string
}

func (r *recordingActorDeleter) AdvanceActorDelete(_ context.Context, actorID string) (bool, error) {
	r.deleted = append(r.deleted, actorID)
	return true, nil
}

func TestProvisionerAdvanceDelete_DeletesGoldenActor(t *testing.T) {
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
			Annotations: map[string]string{
				AnnotationManagedActorTemplate: "true",
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()
	rec := &recordingActorDeleter{}
	p := &Provisioner{Client: kube, Ate: rec}

	var complete bool
	var err error
	for range 5 {
		complete, err = p.AdvanceDelete(context.Background(), ah)
		require.NoError(t, err)
		if complete {
			break
		}
	}
	require.True(t, complete, "AdvanceDelete should finish within a few reconcile passes")
	require.Equal(t, []string{"golden-actor-uuid"}, rec.deleted)

	var got atev1alpha1.ActorTemplate
	require.Error(t, kube.Get(context.Background(), client.ObjectKeyFromObject(tmpl), &got))
}

func TestWorkerPoolDeploymentGoneNotFound(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Provisioner{Client: kube}
	gone, err := p.workerPoolDeploymentGone(context.Background(), types.NamespacedName{Namespace: "kagent", Name: "claw-wp"})
	require.NoError(t, err)
	require.True(t, gone)
}
