package scheduledrun

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const SessionUserID = "scheduled-run"

func TargetAPIGroup(ref corev1.TypedLocalObjectReference) string {
	if ref.APIGroup == nil || *ref.APIGroup == "" {
		return v1alpha2.ScheduledRunTargetAPIGroup
	}
	return *ref.APIGroup
}

func TargetKind(ref corev1.TypedLocalObjectReference) string {
	if ref.Kind == "" {
		return v1alpha2.ScheduledRunTargetKindAgent
	}
	return ref.Kind
}

func ValidateTargetRef(ref corev1.TypedLocalObjectReference) error {
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

func TargetKey(srNamespace string, ref corev1.TypedLocalObjectReference) types.NamespacedName {
	return types.NamespacedName{
		Namespace: srNamespace,
		Name:      ref.Name,
	}
}

func NewTargetObject(kind string) (client.Object, error) {
	switch kind {
	case "", v1alpha2.ScheduledRunTargetKindAgent:
		return &v1alpha2.Agent{}, nil
	case v1alpha2.ScheduledRunTargetKindSandboxAgent:
		return &v1alpha2.SandboxAgent{}, nil
	default:
		return nil, fmt.Errorf("unsupported targetRef.kind %q", kind)
	}
}

func GetTarget(ctx context.Context, kube client.Client, srNamespace string, ref corev1.TypedLocalObjectReference) (client.Object, error) {
	if err := ValidateTargetRef(ref); err != nil {
		return nil, err
	}
	obj, err := NewTargetObject(TargetKind(ref))
	if err != nil {
		return nil, err
	}
	if err := kube.Get(ctx, TargetKey(srNamespace, ref), obj); err != nil {
		return nil, err
	}
	return obj, nil
}
