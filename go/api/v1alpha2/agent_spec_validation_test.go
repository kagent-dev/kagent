package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSubstrateSandboxAgentSpec(t *testing.T) {
	t.Run("allows substrate without skills", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{Platform: SandboxPlatformSubstrate},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("allows skills on agent-sandbox platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform:  SandboxPlatformAgentSandbox,
				AgentSpec: AgentSpec{Skills: &SkillForAgent{Refs: []string{"ghcr.io/org/skill:latest"}}},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("rejects skills on substrate platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform:  SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{Skills: &SkillForAgent{Refs: []string{"ghcr.io/org/skill:latest"}}},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxSkillsUnsupportedMsg)
	})

	t.Run("rejects python runtime on substrate platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_Declarative,
					Declarative: &DeclarativeAgentSpec{
						Runtime: DeclarativeRuntime_Python,
					},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxPythonRuntimeUnsupportedMsg)
	})

	t.Run("rejects BYO agents on substrate platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxBYOUnsupportedMsg)
	})

	t.Run("allows BYO agents on agent-sandbox platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformAgentSandbox,
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{},
				},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("allows go runtime on substrate platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_Declarative,
					Declarative: &DeclarativeAgentSpec{
						Runtime: DeclarativeRuntime_Go,
					},
				},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})
}
