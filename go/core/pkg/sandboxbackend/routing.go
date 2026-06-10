package sandboxbackend

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RoutingBackend delegates to agent-sandbox or Agent Substrate based on spec.platform.
type RoutingBackend struct {
	AgentSandbox Backend
	Substrate    Backend
}

var _ Backend = (*RoutingBackend)(nil)

// NewRoutingBackend returns a backend that routes SandboxAgent workloads by platform.
func NewRoutingBackend(agentSandbox, substrate Backend) *RoutingBackend {
	return &RoutingBackend{AgentSandbox: agentSandbox, Substrate: substrate}
}

func (r *RoutingBackend) backendFor(agent v1alpha2.AgentObject) (Backend, error) {
	if r == nil {
		return nil, fmt.Errorf("routing sandbox backend is nil")
	}
	if v1alpha2.AgentSandboxPlatform(agent) == v1alpha2.SandboxPlatformSubstrate {
		if r.Substrate == nil {
			return nil, fmt.Errorf("substrate sandbox backend is not configured")
		}
		return r.Substrate, nil
	}
	if r.AgentSandbox == nil {
		return nil, fmt.Errorf("agent-sandbox backend is not configured")
	}
	return r.AgentSandbox, nil
}

func (r *RoutingBackend) BuildSandbox(ctx context.Context, in BuildInput) ([]client.Object, error) {
	b, err := r.backendFor(in.Agent)
	if err != nil {
		return nil, err
	}
	return b.BuildSandbox(ctx, in)
}

func (r *RoutingBackend) GetOwnedResourceTypes() []client.Object {
	var out []client.Object
	if r != nil && r.AgentSandbox != nil {
		out = append(out, r.AgentSandbox.GetOwnedResourceTypes()...)
	}
	if r != nil && r.Substrate != nil {
		out = append(out, r.Substrate.GetOwnedResourceTypes()...)
	}
	return out
}

// OwnedResourceTypesFor returns owned-resource types for the agent's sandbox platform.
func (r *RoutingBackend) OwnedResourceTypesFor(agent v1alpha2.AgentObject) ([]client.Object, error) {
	b, err := r.backendFor(agent)
	if err != nil {
		return nil, err
	}
	return b.GetOwnedResourceTypes(), nil
}

func (r *RoutingBackend) ComputeReady(ctx context.Context, cl client.Client, nn types.NamespacedName) (metav1.ConditionStatus, string, string) {
	sa := &v1alpha2.SandboxAgent{}
	if err := cl.Get(ctx, nn, sa); err != nil {
		return metav1.ConditionUnknown, "SandboxAgentNotFound", err.Error()
	}
	b, err := r.backendFor(sa)
	if err != nil {
		return metav1.ConditionUnknown, "SandboxBackendNotConfigured", err.Error()
	}
	return b.ComputeReady(ctx, cl, nn)
}

// ValidatePlatform reports whether this routing backend can reconcile the agent's sandbox platform.
func (r *RoutingBackend) ValidatePlatform(agent v1alpha2.AgentObject) error {
	_, err := r.backendFor(agent)
	return err
}

// ValidateSandboxPlatform reports whether backend supports the agent's sandbox platform.
func ValidateSandboxPlatform(backend Backend, agent v1alpha2.AgentObject) error {
	if backend == nil {
		return fmt.Errorf("sandbox backend is not configured")
	}
	if rb, ok := backend.(*RoutingBackend); ok {
		return rb.ValidatePlatform(agent)
	}
	return nil
}
