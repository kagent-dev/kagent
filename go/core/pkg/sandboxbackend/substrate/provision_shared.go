package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AnnotationManagedWorkerPool    = "kagent.dev/substrate-managed-workerpool"
	AnnotationManagedActorTemplate = "kagent.dev/substrate-managed-actortemplate"

	defaultWorkerPoolReplicas = int32(1)
	defaultSnapshotsBucket    = "ate-snapshots"
	defaultOpenClawContainer  = "openclaw"
)

// ProvisionDefaults are cluster-wide defaults for auto-provisioned Substrate CRs.
type ProvisionDefaults struct {
	PauseImage           string
	RunscAMD64URL        string
	RunscAMD64SHA256     string
	RunscARM64URL        string
	RunscARM64SHA256     string
	DefaultAteomImage    string
	DefaultWorkloadImage string
}

// ateActorDeleter removes actors from ate-api during harness teardown.
type ateActorDeleter interface {
	deleteActorSequenced(ctx context.Context, actorID string) error
}

// Provisioner ensures WorkerPool and ActorTemplate exist for a substrate AgentHarness.
type Provisioner struct {
	Client   client.Client
	Defaults ProvisionDefaults
	// Ate deletes harness and golden snapshot actors before Substrate CRs are removed.
	Ate ateActorDeleter
}

// EnsureResult describes provisioned Substrate resources.
type EnsureResult struct {
	WorkerPoolRef        types.NamespacedName
	ActorTemplateRef     types.NamespacedName
	ActorTemplateReady   bool
	ManagedWorkerPool    bool
	ManagedActorTemplate bool
}

func validateSubstrateProvisionSpec(ah *v1alpha2.AgentHarness) error {
	sub := ah.Spec.Substrate
	if err := ValidateGatewayTokenSpec(sub); err != nil {
		return err
	}
	if sub.ActorTemplateRef != nil && strings.TrimSpace(sub.ActorTemplateRef.Name) != "" {
		return nil
	}
	loc := substrateSnapshotsLocation(ah)
	if !strings.HasPrefix(loc, "gs://") {
		return fmt.Errorf("spec.substrate.snapshotsConfig.location must be a gs:// URI (Substrate snapshots are GCS-only today)")
	}
	if sub.WorkerPoolRef != nil && strings.TrimSpace(sub.WorkerPoolRef.Name) != "" && sub.WorkerPool != nil {
		return fmt.Errorf("spec.substrate.workerPoolRef and workerPool are mutually exclusive")
	}
	return nil
}

func defaultRunscConfig(d ProvisionDefaults) atev1alpha1.RunscConfig {
	return atev1alpha1.RunscConfig{
		AMD64: &atev1alpha1.RunscPlatformConfig{
			URL:        d.RunscAMD64URL,
			SHA256Hash: d.RunscAMD64SHA256,
		},
		ARM64: &atev1alpha1.RunscPlatformConfig{
			URL:        d.RunscARM64URL,
			SHA256Hash: d.RunscARM64SHA256,
		},
	}
}

func substrateSnapshotsLocation(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return defaultSubstrateSnapshotsLocation("", "")
	}
	if sub := ah.Spec.Substrate; sub != nil && sub.SnapshotsConfig != nil {
		if loc := strings.TrimSpace(sub.SnapshotsConfig.Location); loc != "" {
			return loc
		}
	}
	return defaultSubstrateSnapshotsLocation(ah.Namespace, ah.Name)
}

func defaultSubstrateSnapshotsLocation(namespace, name string) string {
	return fmt.Sprintf("gs://%s/%s/%s", defaultSnapshotsBucket, namespace, name)
}

func provisionLabels(ah *v1alpha2.AgentHarness) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kagent",
		"kagent.dev/agent-harness":     ah.Name,
	}
}

func workerPoolName(ah *v1alpha2.AgentHarness) string {
	return truncateDNS1123(ah.Name + "-wp")
}

func actorTemplateName(ah *v1alpha2.AgentHarness) string {
	return truncateDNS1123(ah.Name)
}

func truncateDNS1123(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	return s
}
