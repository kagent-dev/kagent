package sandboxbackend

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

// EnsureAgentSandboxAPIsRegistered checks that the apiserver exposes the agent-sandbox
// resources kagent needs (SandboxTemplate, SandboxClaim, Sandbox). Call this before
// creating or reconciling SandboxAgent when a sandbox backend is configured.
//
// When CRDs are missing, the apiserver returns a *meta.NoKindMatchError (or similar);
// that surfaces as a clear prerequisite error instead of a late reconcile failure.
func EnsureAgentSandboxAPIsRegistered(ctx context.Context, c client.Client) error {
	checks := []struct {
		list client.ObjectList
		kind string
	}{
		{&extensionsv1alpha1.SandboxTemplateList{}, "SandboxTemplate (extensions.agents.x-k8s.io/v1alpha1)"},
		{&extensionsv1alpha1.SandboxClaimList{}, "SandboxClaim (extensions.agents.x-k8s.io/v1alpha1)"},
		{&agentsandboxv1.SandboxList{}, "Sandbox (agents.x-k8s.io/v1alpha1)"},
	}
	for _, ch := range checks {
		if err := c.List(ctx, ch.list, client.Limit(1)); err != nil {
			if meta.IsNoMatchError(err) {
				return fmt.Errorf("agent-sandbox API %s is not available on this cluster; install the agent-sandbox CRDs and controller before using SandboxAgent: %w", ch.kind, err)
			}
			return fmt.Errorf("could not reach agent-sandbox API %s (check RBAC and apiserver connectivity): %w", ch.kind, err)
		}
	}
	return nil
}
