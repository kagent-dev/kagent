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

func TestValidateSubstrateProvisionSpec(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "claw"},
		Spec: v1alpha2.AgentHarnessSpec{
			Runtime: v1alpha2.AgentHarnessRuntimeSubstrate,
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				GatewayToken: "test-token",
				SnapshotsConfig: &v1alpha2.AgentHarnessSubstrateSnapshotsConfig{
					Location: "gs://bucket/prefix/",
				},
			},
		},
	}
	if err := validateSubstrateProvisionSpec(ah); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	ah.Spec.Substrate.SnapshotsConfig = nil
	if err := validateSubstrateProvisionSpec(ah); err != nil {
		t.Fatalf("expected default snapshots config to be valid: %v", err)
	}
	if got := substrateSnapshotsLocation(ah); got != "gs://ate-snapshots/kagent/claw" {
		t.Fatalf("got default snapshots location %q", got)
	}

	ah.Spec.Substrate.GatewayToken = ""
	if err := validateSubstrateProvisionSpec(ah); err == nil {
		t.Fatal("expected error when gateway token is not configured")
	}

	ah.Spec.Substrate.GatewayToken = "test-token"
	ah.Spec.Substrate.SnapshotsConfig = &v1alpha2.AgentHarnessSubstrateSnapshotsConfig{Location: "s3://nope"}
	if err := validateSubstrateProvisionSpec(ah); err == nil {
		t.Fatal("expected error for non-gs location")
	}

	ah.Spec.Substrate.SnapshotsConfig.Location = "gs://ok"
	ah.Spec.Substrate.WorkerPoolRef = &v1alpha2.TypedReference{Name: "pool"}
	ah.Spec.Substrate.WorkerPool = &v1alpha2.AgentHarnessSubstrateWorkerPoolSpec{Replicas: 2}
	if err := validateSubstrateProvisionSpec(ah); err == nil {
		t.Fatal("expected error for workerPoolRef and workerPool together")
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
