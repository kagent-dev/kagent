package scheduledrun

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// SessionUserID groups sessions created by ScheduledRuns under a reserved internal
	// identity. It is a storage key, not an authorization boundary.
	SessionUserID = "scheduled-run"
	// TargetRefIndexField is the cache field index used to resolve ScheduledRuns
	// that reference a watched target.
	TargetRefIndexField    = "scheduledrun.spec.targetRef"
	TargetAPIGroup         = v1alpha2.ScheduledRunTargetAPIGroup
	TargetKindAgent        = v1alpha2.ScheduledRunTargetKindAgent
	TargetKindSandboxAgent = v1alpha2.ScheduledRunTargetKindSandboxAgent
)

// IndexTargetRef returns the normalized key for a ScheduledRun target.
func IndexTargetRef(obj client.Object) []string {
	sr, ok := obj.(*v1alpha2.ScheduledRun)
	if !ok || sr.Spec.TargetRef.Name == "" {
		return nil
	}
	return []string{TargetRefKey(sr.Namespace, sr.Spec.TargetRef)}
}

// TargetRefKey returns the normalized cache key for a target reference.
func TargetRefKey(scheduledRunNamespace string, ref corev1.TypedLocalObjectReference) string {
	return fmt.Sprintf("%s/%s/%s/%s", TargetAPIGroupFor(ref), ref.Kind, scheduledRunNamespace, ref.Name)
}

// ValidateTargetRef validates the local reference used by ScheduledRun.
func ValidateTargetRef(ref corev1.TypedLocalObjectReference) error {
	if ref.Name == "" {
		return fmt.Errorf("targetRef.name is required")
	}
	if group := TargetAPIGroupFor(ref); group != TargetAPIGroup {
		return fmt.Errorf("unsupported targetRef.apiGroup %q", group)
	}
	switch ref.Kind {
	case TargetKindAgent, TargetKindSandboxAgent:
		return nil
	default:
		return fmt.Errorf("unsupported targetRef.kind %q", ref.Kind)
	}
}

func TargetAPIGroupFor(ref corev1.TypedLocalObjectReference) string {
	if ref.APIGroup == nil {
		return ""
	}
	return *ref.APIGroup
}

// TargetKey returns the resolved namespaced name for a target reference.
func TargetKey(scheduledRunNamespace string, ref corev1.TypedLocalObjectReference) types.NamespacedName {
	return types.NamespacedName{Namespace: scheduledRunNamespace, Name: ref.Name}
}

// GetTarget resolves a typed same-namespace target reference.
func GetTarget(ctx context.Context, kube client.Client, scheduledRunNamespace string, ref corev1.TypedLocalObjectReference) (client.Object, error) {
	if err := ValidateTargetRef(ref); err != nil {
		return nil, err
	}
	key := TargetKey(scheduledRunNamespace, ref)
	var target client.Object
	switch ref.Kind {
	case TargetKindAgent:
		target = &v1alpha2.Agent{}
	case TargetKindSandboxAgent:
		target = &v1alpha2.SandboxAgent{}
	default:
		return nil, fmt.Errorf("unsupported targetRef.kind %q", ref.Kind)
	}
	if err := kube.Get(ctx, key, target); err != nil {
		return nil, fmt.Errorf("get %s %s: %w", ref.Kind, key, err)
	}
	return target, nil
}
