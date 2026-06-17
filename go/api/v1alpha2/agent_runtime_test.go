package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveDeclarativeRuntimeForAgent(t *testing.T) {
	substrateSpec := AgentSpec{
		Type: AgentType_Declarative,
		Declarative: &DeclarativeAgentSpec{
			Runtime: DeclarativeRuntime_Python,
		},
	}

	t.Run("regular Agent keeps configured runtime on substrate platform", func(t *testing.T) {
		agent := &Agent{Spec: substrateSpec}
		require.Equal(t, DeclarativeRuntime_Python, EffectiveDeclarativeRuntimeForAgent(agent))
	})

	t.Run("SandboxAgent on substrate honors configured runtime", func(t *testing.T) {
		sa := &SandboxAgent{Spec: SandboxAgentSpec{AgentSpec: substrateSpec, Platform: SandboxPlatformSubstrate}}
		require.Equal(t, DeclarativeRuntime_Python, EffectiveDeclarativeRuntimeForAgent(sa))
	})

	t.Run("SandboxAgent on substrate honors Go runtime when set", func(t *testing.T) {
		goSpec := AgentSpec{Type: AgentType_Declarative, Declarative: &DeclarativeAgentSpec{Runtime: DeclarativeRuntime_Go}}
		sa := &SandboxAgent{Spec: SandboxAgentSpec{AgentSpec: goSpec, Platform: SandboxPlatformSubstrate}}
		require.Equal(t, DeclarativeRuntime_Go, EffectiveDeclarativeRuntimeForAgent(sa))
	})

	t.Run("SandboxAgent on agent-sandbox keeps configured runtime", func(t *testing.T) {
		sa := &SandboxAgent{Spec: SandboxAgentSpec{AgentSpec: substrateSpec, Platform: SandboxPlatformAgentSandbox}}
		require.Equal(t, DeclarativeRuntime_Python, EffectiveDeclarativeRuntimeForAgent(sa))
	})

	t.Run("regular Agent honors Go runtime", func(t *testing.T) {
		agent := &Agent{Spec: AgentSpec{
			Type: AgentType_Declarative,
			Declarative: &DeclarativeAgentSpec{
				Runtime: DeclarativeRuntime_Go,
			},
		}}
		require.Equal(t, DeclarativeRuntime_Go, EffectiveDeclarativeRuntimeForAgent(agent))
	})
}
