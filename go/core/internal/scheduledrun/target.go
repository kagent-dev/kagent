package scheduledrun

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TargetKind(ref v1alpha2.AgentReference) v1alpha2.AgentReferenceKind {
	if ref.Kind == "" {
		return v1alpha2.AgentReferenceKindAgent
	}
	return ref.Kind
}

func TargetNamespace(srNamespace string, ref v1alpha2.AgentReference) string {
	if ref.Namespace != "" {
		return ref.Namespace
	}
	return srNamespace
}

func TargetKey(srNamespace string, ref v1alpha2.AgentReference) types.NamespacedName {
	return types.NamespacedName{
		Namespace: TargetNamespace(srNamespace, ref),
		Name:      ref.Name,
	}
}

func NewTargetObject(kind v1alpha2.AgentReferenceKind) (client.Object, error) {
	switch kind {
	case "", v1alpha2.AgentReferenceKindAgent:
		return &v1alpha2.Agent{}, nil
	case v1alpha2.AgentReferenceKindSandboxAgent:
		return &v1alpha2.SandboxAgent{}, nil
	default:
		return nil, fmt.Errorf("unsupported agentRef.kind %q", kind)
	}
}

func GetTarget(ctx context.Context, kube client.Client, srNamespace string, ref v1alpha2.AgentReference) (client.Object, error) {
	obj, err := NewTargetObject(TargetKind(ref))
	if err != nil {
		return nil, err
	}
	if err := kube.Get(ctx, TargetKey(srNamespace, ref), obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func ValidateSameNamespace(srNamespace string, ref v1alpha2.AgentReference) error {
	targetNamespace := TargetNamespace(srNamespace, ref)
	if targetNamespace != srNamespace {
		return fmt.Errorf("agentRef.namespace %q must match ScheduledRun namespace %q", targetNamespace, srNamespace)
	}
	return nil
}
