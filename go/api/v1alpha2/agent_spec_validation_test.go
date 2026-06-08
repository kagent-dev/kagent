package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSubstrateSandboxAgentSpec(t *testing.T) {
	t.Run("allows substrate without skills", func(t *testing.T) {
		spec := &AgentSpec{
			Sandbox: &SandboxConfig{Platform: SandboxPlatformSubstrate},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(spec))
	})

	t.Run("allows skills on agent-sandbox platform", func(t *testing.T) {
		spec := &AgentSpec{
			Sandbox: &SandboxConfig{Platform: SandboxPlatformAgentSandbox},
			Skills:  &SkillForAgent{Refs: []string{"ghcr.io/org/skill:latest"}},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(spec))
	})

	t.Run("rejects skills on substrate platform", func(t *testing.T) {
		spec := &AgentSpec{
			Sandbox: &SandboxConfig{Platform: SandboxPlatformSubstrate},
			Skills:  &SkillForAgent{Refs: []string{"ghcr.io/org/skill:latest"}},
		}
		err := ValidateSubstrateSandboxAgentSpec(spec)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxSkillsUnsupportedMsg)
	})

	t.Run("rejects python runtime on substrate platform", func(t *testing.T) {
		spec := &AgentSpec{
			Type:    AgentType_Declarative,
			Sandbox: &SandboxConfig{Platform: SandboxPlatformSubstrate},
			Declarative: &DeclarativeAgentSpec{
				Runtime: DeclarativeRuntime_Python,
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(spec)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxPythonRuntimeUnsupportedMsg)
	})

	t.Run("allows go runtime on substrate platform", func(t *testing.T) {
		spec := &AgentSpec{
			Type:    AgentType_Declarative,
			Sandbox: &SandboxConfig{Platform: SandboxPlatformSubstrate},
			Declarative: &DeclarativeAgentSpec{
				Runtime: DeclarativeRuntime_Go,
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(spec))
	})
}
