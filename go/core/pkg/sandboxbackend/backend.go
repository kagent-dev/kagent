package sandboxbackend

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildInput carries the pod template for a Sandbox workload (agents.x-k8s.io Sandbox).
type BuildInput struct {
	Agent        v1alpha2.AgentObject
	PodTemplate  corev1.PodTemplateSpec
	WorkloadName string
	ExtraLabels  map[string]string
	ConfigData   map[string]string
}

// Backend builds sandbox CRD objects and evaluates their readiness.
type Backend interface {
	BuildSandbox(ctx context.Context, in BuildInput) ([]client.Object, error)
	GetOwnedResourceTypes() []client.Object

	// ComputeReady reflects implementation-specific status into condition pieces for Agent.status.
	ComputeReady(ctx context.Context, cl client.Client, nn types.NamespacedName) (status metav1.ConditionStatus, reason, message string)

	// EnsureAPIsRegistered verifies that any cluster-scoped APIs the backend depends on are
	// reachable. Backends that don't depend on cluster APIs (e.g. those that talk to an
	// out-of-cluster service) should return nil.
	EnsureAPIsRegistered(ctx context.Context, c client.Client) error
}
