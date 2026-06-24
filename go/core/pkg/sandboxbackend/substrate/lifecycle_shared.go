package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
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
		return substrateSnapshotsLocationFor("", "", "")
	}
	loc := ""
	if sub := ah.Spec.Substrate; sub != nil && sub.SnapshotsConfig != nil {
		loc = sub.SnapshotsConfig.Location
	}
	return substrateSnapshotsLocationFor(ah.Namespace, ah.Name, loc)
}

func substrateSnapshotsLocationFor(namespace, name, explicitLocation string) string {
	if loc := strings.TrimSpace(explicitLocation); loc != "" {
		return loc
	}
	return defaultSubstrateSnapshotsLocation(namespace, name)
}

func (p *Lifecycle) resolveWorkerPoolRefFor(
	ctx context.Context,
	namespace string,
	explicit *v1alpha2.TypedLocalReference,
) (types.NamespacedName, error) {
	if p == nil || p.Client == nil {
		return types.NamespacedName{}, fmt.Errorf("substrate lifecycle kubernetes client is required")
	}
	key := p.Defaults.DefaultWorkerPool
	if explicit != nil {
		if name := strings.TrimSpace(explicit.Name); name != "" {
			key = types.NamespacedName{Namespace: namespace, Name: name}
		}
	}
	if key.Name == "" {
		return types.NamespacedName{}, fmt.Errorf("substrate workerPoolRef is required when no default WorkerPool is configured")
	}
	if key.Namespace == "" {
		key.Namespace = namespace
	}

	var wp atev1alpha1.WorkerPool
	if err := p.Client.Get(ctx, key, &wp); err != nil {
		return types.NamespacedName{}, fmt.Errorf("get WorkerPool %s: %w", key, err)
	}
	return key, nil
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

// actorTemplateEnvFromPodEnv converts pod env vars into ActorTemplate env vars.
// Substrate ActorTemplates only support literal values, secretKeyRef, and configMapKeyRef.
func actorTemplateEnvFromPodEnv(env []corev1.EnvVar) []atev1alpha1.EnvVar {
	out := make([]atev1alpha1.EnvVar, 0, len(env))
	seen := make(map[string]struct{}, len(env))
	for _, e := range env {
		if e.Name == "" {
			continue
		}
		sanitized := sanitizeActorTemplateEnvVar(e)
		if sanitized == nil {
			continue
		}
		if _, ok := seen[sanitized.Name]; ok {
			continue
		}
		seen[sanitized.Name] = struct{}{}
		out = append(out, *sanitized)
	}
	return out
}

func sanitizeActorTemplateEnvVar(e corev1.EnvVar) *atev1alpha1.EnvVar {
	if e.Value != "" {
		return &atev1alpha1.EnvVar{
			Name:      e.Name,
			ValueFrom: nil,
			Value:     &e.Value,
		}
	}
	if ref := e.ValueFrom.SecretKeyRef; ref != nil {
		return &atev1alpha1.EnvVar{
			Name: e.Name,
			ValueFrom: &atev1alpha1.EnvVarSource{
				SecretKeyRef: &atev1alpha1.SecretKeySelector{
					Name:     ref.Name,
					Key:      ref.Key,
					Optional: ref.Optional,
				},
			},
		}
	}
	return nil
}
