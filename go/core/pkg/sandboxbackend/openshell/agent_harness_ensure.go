package openshell

import (
	"context"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
)

type createSandboxRequestBuilder func(*v1alpha2.AgentHarness, []string) (*openshellv1.CreateSandboxRequest, []string)

// ensureAgentHarnessSandbox translates model config, upserts messaging providers, and creates the sandbox.
func (b *agentHarnessOpenShellBackend) ensureAgentHarnessSandbox(
	ctx context.Context,
	ah *v1alpha2.AgentHarness,
	build createSandboxRequestBuilder,
) (sandboxbackend.EnsureResult, error) {
	if err := translateModelConfig(ctx, ah, b.kubeClient, b.clients); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}
	providerNames, err := UpsertMessagingProviders(ctx, b.clients, b.kubeClient, ah)
	if err != nil {
		return sandboxbackend.EnsureResult{}, err
	}
	req, unsupported := build(ah, providerNames)
	return b.CreateAgentHarnessSandbox(ctx, ah, req, unsupported)
}

func attachMessagingProviders(req *openshellv1.CreateSandboxRequest, names []string) {
	if req == nil || len(names) == 0 {
		return
	}
	if req.Spec == nil {
		req.Spec = &openshellv1.SandboxSpec{}
	}
	req.Spec.Providers = append(req.Spec.Providers, names...)
}
