package substrate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultSnapshotsBucket = "ate-snapshots"

	// Referenced by generated ActorTemplates to gate scheduling onto a WorkerPool.
	// The kagent Helm chart stamps it on the WorkerPool it manages;
	// externally-owned pools must carry it to remain eligible.
	WorkerPoolLabelKey = "kagent.dev/worker-pool"
)

// LifecycleDefaults are cluster-wide defaults for generated ActorTemplate lifecycle.
type LifecycleDefaults struct {
	DefaultWorkerPool types.NamespacedName
}

// Lifecycle reconciles the Kubernetes lifecycle that kagent owns for substrate-backed agents.
// WorkerPools are externally owned; this helper only resolves the selected WorkerPool.
type Lifecycle struct {
	Client    client.Client
	Defaults  LifecycleDefaults
	AteClient *Client
}

func NewLifecycle(kube client.Client, defaults LifecycleDefaults, ateClient *Client) *Lifecycle {
	return &Lifecycle{
		Client:    kube,
		Defaults:  defaults,
		AteClient: ateClient,
	}
}

func workerSelectorForPool(wpKey types.NamespacedName) *metav1.LabelSelector {
	if wpKey.Name == "" {
		return nil
	}
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{WorkerPoolLabelKey: wpKey.Name},
	}
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
	explicit *v1alpha3.TypedLocalReference,
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
	return selectCurrentActorTemplate(templates), nil
}

// selectCurrentActorTemplate selects the current actor as defined by the
// highest-desired-generation template whose golden is Ready
func selectCurrentActorTemplate(templates []*atev1alpha1.ActorTemplate) *atev1alpha1.ActorTemplate {
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
		return desiredReady
	}
	return desired
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

// actorTemplateEnvFromPodEnv converts resolved pod env vars into ActorTemplate env vars.
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
	if e.ValueFrom != nil {
		return nil
	}
	return &atev1alpha1.EnvVar{Name: e.Name, Value: e.Value}
}

func resolvePodEnv(ctx context.Context, kube client.Reader, namespace string, env []corev1.EnvVar, localSecret *corev1.Secret) ([]corev1.EnvVar, error) {
	resolved := append([]corev1.EnvVar(nil), env...)
	for i, variable := range resolved {
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		secret := &corev1.Secret{}
		if localSecret != nil && localSecret.Name == ref.Name {
			secret = localSecret
		} else if err := kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, secret); err != nil {
			if ref.Optional != nil && *ref.Optional && apierrors.IsNotFound(err) {
				resolved[i].ValueFrom = nil
				continue
			}
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			if ref.Optional != nil && *ref.Optional {
				resolved[i].ValueFrom = nil
				continue
			}
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		resolved[i].Value = string(value)
		resolved[i].ValueFrom = nil
	}
	return resolved, nil
}
