package substrate

import (
	"context"
	"fmt"
	"strconv"
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
// from: the template matching the agent's CURRENT desired config whose golden is Ready, else the
// most-recently-desired Ready template (the previous config) while the desired one is still
// building — the blue-green pivot, with no downtime and an atomic flip once the new golden is
// Ready.
//
// "Desired" is tracked by the kagent.dev/desired-generation annotation (the agent generation that
// last applied the template), NOT creationTimestamp. Creation time is wrong for a flip-back to a
// retained older config: that template's golden was built earlier, so by-creation ordering would
// keep serving the newer (now-undesired) config. The desired template is always re-applied with
// the current (highest) generation, so picking the highest-generation Ready template follows the
// current config in both directions. Falls back to the highest-generation template when none is
// Ready yet (first build). Returns (nil, nil) when no template exists.
func ResolveCurrentActorTemplate(ctx context.Context, kube client.Client, namespace, agentName string) (*atev1alpha1.ActorTemplate, error) {
	templates, err := listSandboxAgentActorTemplates(ctx, kube, namespace, agentName)
	if err != nil {
		return nil, err
	}
	var desiredReady, desired *atev1alpha1.ActorTemplate
	for i := range templates {
		t := templates[i]
		if desired == nil || moreDesiredActorTemplate(t, desired) {
			desired = t
		}
		if t.Status.Phase == atev1alpha1.PhaseReady {
			if desiredReady == nil || moreDesiredActorTemplate(t, desiredReady) {
				desiredReady = t
			}
		}
	}
	if desiredReady != nil {
		return desiredReady, nil
	}
	return desired, nil
}

// moreDesiredActorTemplate reports whether a is "more desired" than b: a higher desired-generation
// wins (the template applied for the current config), with creationTimestamp as a tiebreaker for
// legacy templates that predate the annotation.
func moreDesiredActorTemplate(a, b *atev1alpha1.ActorTemplate) bool {
	ga, gb := actorTemplateDesiredGeneration(a), actorTemplateDesiredGeneration(b)
	if ga != gb {
		return ga > gb
	}
	return a.CreationTimestamp.After(b.CreationTimestamp.Time)
}

// actorTemplateDesiredGeneration parses the desired-generation annotation; absent/invalid is 0.
func actorTemplateDesiredGeneration(t *atev1alpha1.ActorTemplate) int64 {
	g, err := strconv.ParseInt(t.Annotations[desiredGenerationAnnotation], 10, 64)
	if err != nil {
		return 0
	}
	return g
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
