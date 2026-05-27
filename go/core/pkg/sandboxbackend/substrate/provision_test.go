package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestSubstrateSnapshotsLocationDefault(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "claw"},
		Spec: v1alpha2.AgentHarnessSpec{
			Runtime: v1alpha2.AgentHarnessRuntimeSubstrate,
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				GatewayToken: "test-token",
			},
		},
	}
	if got := substrateSnapshotsLocation(ah); got != "gs://ate-snapshots/kagent/claw" {
		t.Fatalf("got default snapshots location %q", got)
	}
}

func TestEnsureWorkerPoolUsesDefaultAteomImage(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		defaultImg  string
		workerPool  *v1alpha2.AgentHarnessSubstrateWorkerPoolSpec
		wantImage   string
		wantReplica int32
	}{
		{
			name:        "defaults omitted replicas",
			defaultImg:  "registry.example/ateom:default",
			workerPool:  &v1alpha2.AgentHarnessSubstrateWorkerPoolSpec{},
			wantImage:   "registry.example/ateom:default",
			wantReplica: 1,
		},
		{
			name:        "workerpool override",
			defaultImg:  "registry.example/ateom:default",
			workerPool:  &v1alpha2.AgentHarnessSubstrateWorkerPoolSpec{Replicas: 3, AteomImage: "registry.example/ateom:override"},
			wantImage:   "registry.example/ateom:override",
			wantReplica: 3,
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
					Runtime: v1alpha2.AgentHarnessRuntimeSubstrate,
					Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
						WorkerPool: tt.workerPool,
					},
				},
			}
			p := &Provisioner{
				Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
				Defaults: ProvisionDefaults{DefaultAteomImage: tt.defaultImg},
			}

			key, managed, err := p.ensureWorkerPool(context.Background(), ah)
			require.NoError(t, err)
			require.True(t, managed)

			var wp atev1alpha1.WorkerPool
			require.NoError(t, p.Client.Get(context.Background(), key, &wp))
			require.Equal(t, tt.wantImage, wp.Spec.AteomImage)
			require.Equal(t, tt.wantReplica, wp.Spec.Replicas)
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
