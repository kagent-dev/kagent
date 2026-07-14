package scheduledrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// SessionUserID groups sessions created by ScheduledRuns under a shared identity.
	SessionUserID = "scheduled-run"
	// TargetRefIndexField is the cache field index used to resolve ScheduledRuns
	// that reference a watched target.
	TargetRefIndexField = "scheduledrun.spec.targetRef"
)

// ErrTargetAccessDenied identifies a valid cross-namespace reference that the
// target's AllowedNamespaces policy does not permit.
var ErrTargetAccessDenied = errors.New("target access denied")

// IndexTargetRef returns the normalized key for a ScheduledRun target.
func IndexTargetRef(obj client.Object) []string {
	sr, ok := obj.(*v1alpha2.ScheduledRun)
	if !ok || sr.Spec.TargetRef.Name == "" {
		return nil
	}
	return []string{TargetRefKey(sr.Namespace, sr.Spec.TargetRef)}
}

// TargetAPIGroup returns the target API group, applying the ScheduledRun default.
func TargetAPIGroup(ref corev1.TypedObjectReference) string {
	if ref.APIGroup == nil || *ref.APIGroup == "" {
		return v1alpha2.ScheduledRunTargetAPIGroup
	}
	return *ref.APIGroup
}

// TargetKind returns the target kind, applying the ScheduledRun default.
func TargetKind(ref corev1.TypedObjectReference) string {
	if ref.Kind == "" {
		return v1alpha2.ScheduledRunTargetKindAgent
	}
	return ref.Kind
}

// TargetNamespace returns the explicit target namespace or the ScheduledRun
// namespace when the reference omits it.
func TargetNamespace(scheduledRunNamespace string, ref corev1.TypedObjectReference) string {
	if ref.Namespace != nil && *ref.Namespace != "" {
		return *ref.Namespace
	}
	return scheduledRunNamespace
}

// TargetRefKey returns the normalized cache key for a target reference.
func TargetRefKey(scheduledRunNamespace string, ref corev1.TypedObjectReference) string {
	return fmt.Sprintf("%s/%s/%s/%s", TargetAPIGroup(ref), TargetKind(ref), TargetNamespace(scheduledRunNamespace, ref), ref.Name)
}

// ValidateTargetRef validates the target types currently supported by ScheduledRun.
func ValidateTargetRef(ref corev1.TypedObjectReference) error {
	if group := TargetAPIGroup(ref); group != v1alpha2.ScheduledRunTargetAPIGroup {
		return fmt.Errorf("unsupported targetRef.apiGroup %q", group)
	}
	if ref.Name == "" {
		return fmt.Errorf("targetRef.name is required")
	}
	switch TargetKind(ref) {
	case v1alpha2.ScheduledRunTargetKindAgent, v1alpha2.ScheduledRunTargetKindSandboxAgent:
		return nil
	default:
		return fmt.Errorf("unsupported targetRef.kind %q", TargetKind(ref))
	}
}

// TargetKey returns the resolved namespaced name for a target reference.
func TargetKey(scheduledRunNamespace string, ref corev1.TypedObjectReference) types.NamespacedName {
	return types.NamespacedName{
		Namespace: TargetNamespace(scheduledRunNamespace, ref),
		Name:      ref.Name,
	}
}

func newTargetObject(kind string) (client.Object, error) {
	switch kind {
	case v1alpha2.ScheduledRunTargetKindAgent:
		return &v1alpha2.Agent{}, nil
	case v1alpha2.ScheduledRunTargetKindSandboxAgent:
		return &v1alpha2.SandboxAgent{}, nil
	default:
		return nil, fmt.Errorf("unsupported targetRef.kind %q", kind)
	}
}

// ValidateTargetNamespaceAccess applies the target resource's AllowedNamespaces
// policy to cross-namespace references.
func ValidateTargetNamespaceAccess(ctx context.Context, kube client.Client, scheduledRunNamespace string, target client.Object) error {
	if target.GetNamespace() == scheduledRunNamespace {
		return nil
	}

	var (
		kind              string
		allowedNamespaces *v1alpha2.AllowedNamespaces
	)
	switch typedTarget := target.(type) {
	case *v1alpha2.Agent:
		kind = v1alpha2.ScheduledRunTargetKindAgent
		allowedNamespaces = typedTarget.Spec.AllowedNamespaces
	case *v1alpha2.SandboxAgent:
		kind = v1alpha2.ScheduledRunTargetKindSandboxAgent
		allowedNamespaces = typedTarget.Spec.AllowedNamespaces
	default:
		return fmt.Errorf("unsupported target object type %T", target)
	}

	allowed, err := allowedNamespaces.AllowsNamespace(ctx, kube, scheduledRunNamespace, target.GetNamespace())
	if err != nil {
		return fmt.Errorf("failed to check cross-namespace reference for %s %s/%s: %w", kind, target.GetNamespace(), target.GetName(), err)
	}
	if !allowed {
		return fmt.Errorf(
			"%w: cross-namespace reference to %s %s/%s is not allowed from namespace %s",
			ErrTargetAccessDenied,
			kind,
			target.GetNamespace(),
			target.GetName(),
			scheduledRunNamespace,
		)
	}
	return nil
}

// GetTarget resolves and fetches a currently supported ScheduledRun target.
func GetTarget(ctx context.Context, kube client.Client, scheduledRunNamespace string, ref corev1.TypedObjectReference) (client.Object, error) {
	if err := ValidateTargetRef(ref); err != nil {
		return nil, err
	}
	obj, err := newTargetObject(TargetKind(ref))
	if err != nil {
		return nil, err
	}
	if err := kube.Get(ctx, TargetKey(scheduledRunNamespace, ref), obj); err != nil {
		return nil, err
	}
	return obj, nil
}
