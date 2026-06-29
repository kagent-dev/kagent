package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestSubstrateSnapshotsLocationDefault(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "claw"},
		Spec: v1alpha2.AgentHarnessSpec{
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{},
		},
	}
	if got := substrateSnapshotsLocation(ah); got != "gs://ate-snapshots/kagent/claw" {
		t.Fatalf("got default snapshots location %q", got)
	}
}

func TestResolveWorkerPoolRef(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		refName    string
		defaultRef types.NamespacedName
		wantRef    types.NamespacedName
	}{
		{
			name:       "uses default workerpool",
			defaultRef: types.NamespacedName{Namespace: "kagent", Name: "default-wp"},
			wantRef:    types.NamespacedName{Namespace: "kagent", Name: "default-wp"},
		},
		{
			name:       "spec workerpool overrides default",
			refName:    "custom-wp",
			defaultRef: types.NamespacedName{Namespace: "kagent", Name: "default-wp"},
			wantRef:    types.NamespacedName{Namespace: "kagent", Name: "custom-wp"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			utilruntime.Must(v1alpha2.AddToScheme(scheme))
			utilruntime.Must(atev1alpha1.AddToScheme(scheme))

			ah := &v1alpha2.AgentHarness{
				TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha2.GroupVersion.String(), Kind: "AgentHarness"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "claw"},
				Spec: v1alpha2.AgentHarnessSpec{
					Substrate: &v1alpha2.AgentHarnessSubstrateSpec{},
				},
			}
			if tt.refName != "" {
				ah.Spec.Substrate.WorkerPoolRef = &v1alpha2.TypedLocalReference{Name: tt.refName}
			}
			wp := &atev1alpha1.WorkerPool{
				ObjectMeta: metav1.ObjectMeta{Name: tt.wantRef.Name, Namespace: tt.wantRef.Namespace},
				Spec: atev1alpha1.WorkerPoolSpec{
					Replicas:   1,
					AteomImage: "registry.example/ateom:default",
				},
			}
			p := &Lifecycle{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(wp).Build(),
				Defaults: LifecycleDefaults{
					DefaultWorkerPool: tt.defaultRef,
				},
			}

			key, err := p.resolveWorkerPoolRef(context.Background(), ah)
			require.NoError(t, err)
			require.Equal(t, tt.wantRef, key)
		})
	}
}

func TestActorTemplateName(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{ObjectMeta: metav1.ObjectMeta{Name: "my-claw"}}
	if got := actorTemplateName(ah); got != "my-claw" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureActorTemplateDoesNotUpdateWhenDesiredStateMatches(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	var updateCalls int
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
				if _, ok := obj.(*atev1alpha1.ActorTemplate); ok {
					updateCalls++
				}
				return c.Update(ctx, obj, opts...)
			},
		}).
		Build()

	ah := &v1alpha2.AgentHarness{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha2.GroupVersion.String(), Kind: "AgentHarness"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kagent",
			Name:      "claw",
			UID:       "00000000-0000-0000-0000-000000000001",
		},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{

				WorkloadImage: "ghcr.io/kagent-dev/kagent/acp-sandbox-openclaw@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}
	lifecycle := &Lifecycle{Client: kube}
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "default-wp"}

	_, err := lifecycle.ensureActorTemplate(context.Background(), ah, wpKey)
	require.NoError(t, err)
	_, err = lifecycle.ensureActorTemplate(context.Background(), ah, wpKey)
	require.NoError(t, err)
	require.Zero(t, updateCalls, "matching desired ActorTemplate should not be updated")
}

func TestReconcileActorTemplateRecreatesOnSpecDrift(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	key := types.NamespacedName{Namespace: "kagent", Name: "claw"}
	baseSpec := func(image string) atev1alpha1.ActorTemplateSpec {
		return atev1alpha1.ActorTemplateSpec{
			PauseImage:   "registry.example/pause@sha256:" + "a0",
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			Containers: []atev1alpha1.Container{{
				Name:  "openclaw",
				Image: image,
			}},
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{Location: "gs://ate-snapshots/kagent/claw"},
		}
	}

	oldImage := "registry.example/acp@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newImage := "registry.example/acp@sha256:2222222222222222222222222222222222222222222222222222222222222222"

	existing := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       baseSpec(oldImage),
		Status:     atev1alpha1.ActorTemplateStatus{GoldenActorID: "golden-1", Phase: atev1alpha1.PhaseReady},
	}

	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&atev1alpha1.ActorTemplate{}).
		WithObjects(existing).
		Build()
	rec := &recordingActorClient{}
	ate := &Client{ControlClient: rec}

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       baseSpec(newImage),
	}

	var got atev1alpha1.ActorTemplate
	var recreated bool
	for range 5 {
		require.NoError(t, reconcileActorTemplate(context.Background(), kube, ate, desired.DeepCopy()))
		if err := kube.Get(context.Background(), key, &got); err == nil && got.Spec.Containers[0].Image == newImage {
			recreated = true
			break
		}
	}

	require.True(t, recreated, "ActorTemplate should be recreated with the new image")
	require.Equal(t, []string{"golden-1"}, rec.deleted, "golden actor must be deleted before recreate")
	require.Empty(t, got.Status.GoldenActorID, "recreated template starts without a golden actor")
}
