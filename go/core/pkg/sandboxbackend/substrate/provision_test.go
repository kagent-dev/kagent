package substrate

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestValidateSubstrateProvisionSpec(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "claw"},
		Spec: v1alpha2.AgentHarnessSpec{
			Runtime: v1alpha2.AgentHarnessRuntimeSubstrate,
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				SnapshotsConfig: v1alpha2.AgentHarnessSubstrateSnapshotsConfig{
					Location: "gs://bucket/prefix/",
				},
			},
		},
	}
	if err := validateSubstrateProvisionSpec(ah); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	ah.Spec.Substrate.SnapshotsConfig.Location = "s3://nope"
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

func TestActorTemplateName(t *testing.T) {
	t.Parallel()
	ah := &v1alpha2.AgentHarness{ObjectMeta: metav1.ObjectMeta{Name: "my-claw"}}
	if got := actorTemplateName(ah); got != "my-claw" {
		t.Fatalf("got %q", got)
	}
}
