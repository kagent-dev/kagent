package sandboxbackend_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/agentsxk8s"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateSandboxPlatform(t *testing.T) {
	t.Parallel()

	routing := sandboxbackend.NewRoutingBackend(agentsxk8s.New(), substrate.NewAgentsBackend(nil, nil))

	substrateSA := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
		Spec: v1alpha2.SandboxAgentSpec{
			Platform: v1alpha2.SandboxPlatformSubstrate,
		},
	}
	require.NoError(t, sandboxbackend.ValidateSandboxPlatform(routing, substrateSA))

	k8sSA := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
		Spec: v1alpha2.SandboxAgentSpec{
			Platform: v1alpha2.SandboxPlatformAgentSandbox,
		},
	}
	require.NoError(t, sandboxbackend.ValidateSandboxPlatform(routing, k8sSA))

	substrateOnly := sandboxbackend.NewRoutingBackend(nil, substrate.NewAgentsBackend(nil, nil))
	require.NoError(t, sandboxbackend.ValidateSandboxPlatform(substrateOnly, substrateSA))
	require.Error(t, sandboxbackend.ValidateSandboxPlatform(substrateOnly, k8sSA))

	agentSandboxOnly := sandboxbackend.NewRoutingBackend(agentsxk8s.New(), nil)
	require.Error(t, sandboxbackend.ValidateSandboxPlatform(agentSandboxOnly, substrateSA))
	require.NoError(t, sandboxbackend.ValidateSandboxPlatform(agentSandboxOnly, k8sSA))
}
