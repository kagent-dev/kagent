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
	defaultSnapshotsBucket   = "ate-snapshots"
	defaultOpenClawContainer = "openclaw"
)

// LifecycleDefaults are cluster-wide defaults for generated ActorTemplate lifecycle.
type LifecycleDefaults struct {
	PauseImage           string
	RunscAMD64URL        string
	RunscAMD64SHA256     string
	RunscARM64URL        string
	RunscARM64SHA256     string
	DefaultWorkloadImage string
	DefaultWorkerPool    types.NamespacedName
}

// Lifecycle reconciles the Kubernetes lifecycle that kagent owns for a substrate AgentHarness.
// WorkerPools are externally owned; this helper only resolves the selected WorkerPool.
type Lifecycle struct {
	Client    client.Client
	Defaults  LifecycleDefaults
	AteClient *Client
}

// AgentHarnessLifecycle is the substrate lifecycle surface used by the
// AgentHarness controller.
type AgentHarnessLifecycle interface {
	EnsureGeneratedTemplate(ctx context.Context, ah *v1alpha2.AgentHarness) (LifecycleState, error)
	CleanupGeneratedTemplate(ctx context.Context, ah *v1alpha2.AgentHarness) (bool, error)
}

var _ AgentHarnessLifecycle = (*Lifecycle)(nil)

func NewLifecycle(kube client.Client, defaults LifecycleDefaults, ateClient *Client) *Lifecycle {
	return &Lifecycle{
		Client:    kube,
		Defaults:  defaults,
		AteClient: ateClient,
	}
}

// LifecycleState describes the generated Substrate lifecycle for an AgentHarness.
type LifecycleState struct {
	ActorTemplateReady bool
}

func defaultRunscConfig(d LifecycleDefaults) atev1alpha1.RunscConfig {
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

func lifecycleLabels(ah *v1alpha2.AgentHarness) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kagent",
		"kagent.dev/agent-harness":     ah.Name,
	}
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

// pinImageRef ensures image refs satisfy Substrate ActorTemplate validation (must contain "@").
func pinImageRef(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("workload image is required")
	}
	if !strings.Contains(image, "@") {
		return "", fmt.Errorf("workload image %q must be pinned with a digest (@sha256:...)", image)
	}
	return image, nil
}
