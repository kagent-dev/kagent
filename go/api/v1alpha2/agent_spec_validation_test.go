package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSubstrateSandboxAgentSpec(t *testing.T) {
	t.Run("allows sandbox agent without skills", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("rejects skills", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				AgentSpec: AgentSpec{Skills: &SkillForAgent{Refs: []string{"ghcr.io/org/skill:latest"}}},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxSkillsUnsupportedMsg)
	})

	t.Run("rejects python runtime", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
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

	t.Run("rejects BYO agents", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
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

	t.Run("allows go runtime", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
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
