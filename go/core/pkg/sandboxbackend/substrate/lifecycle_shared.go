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
	return truncateDNS1123To(s, 63)
}

func truncateDNS1123To(s string, max int) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if len(s) > max {
		s = strings.TrimRight(s[:max], "-")
	}
	return s
}

// ResolveCurrentActorTemplate returns the ActorTemplate a SandboxAgent should currently serve
// from: the newest non-terminating template whose golden snapshot is Ready. This is the
// blue-green pivot — during a config change the new template builds while this keeps returning
// the previous Ready template, so chat and readiness stay on the working golden with no downtime
// and flip atomically once the new golden is Ready. Falls back to the newest template when none
// is Ready yet (the very first build). Returns (nil, nil) when no template exists.
func ResolveCurrentActorTemplate(ctx context.Context, kube client.Client, namespace, agentName string) (*atev1alpha1.ActorTemplate, error) {
	templates, err := listSandboxAgentActorTemplates(ctx, kube, namespace, agentName)
	if err != nil {
		return nil, err
	}
	var newestReady, newest *atev1alpha1.ActorTemplate
	for i := range templates {
		t := templates[i]
		if newest == nil || t.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = t
		}
		if t.Status.Phase == atev1alpha1.PhaseReady {
			if newestReady == nil || t.CreationTimestamp.After(newestReady.CreationTimestamp.Time) {
				newestReady = t
			}
		}
	}
	if newestReady != nil {
		return newestReady, nil
	}
	return newest, nil
}

// listSandboxAgentActorTemplates returns the non-terminating generated ActorTemplates for an agent.
func listSandboxAgentActorTemplates(ctx context.Context, kube client.Client, namespace, agentName string) ([]*atev1alpha1.ActorTemplate, error) {
	if kube == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	list := &atev1alpha1.ActorTemplateList{}
	if err := kube.List(ctx, list,
		client.InNamespace(namespace),
		client.MatchingLabels{SandboxAgentLabelKey: agentName},
	); err != nil {
		return nil, fmt.Errorf("list ActorTemplates for %s/%s: %w", namespace, agentName, err)
	}
	out := make([]*atev1alpha1.ActorTemplate, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].DeletionTimestamp.IsZero() {
			out = append(out, &list.Items[i])
		}
	}
	return out, nil
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
