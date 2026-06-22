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

	t.Run("allows python runtime on substrate platform", func(t *testing.T) {
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
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("rejects BYO agents without an explicit command on substrate platform", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{Deployment: &ByoDeploymentSpec{Image: "example/agent:latest"}},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxBYOMissingCommandMsg)
	})

	t.Run("rejects BYO agents with a whitespace-only command on substrate platform", func(t *testing.T) {
		cmd := "   "
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{Deployment: &ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxBYOMissingCommandMsg)
	})

	t.Run("allows BYO agents with an explicit command on substrate platform", func(t *testing.T) {
		cmd := "/app"
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				Platform: SandboxPlatformSubstrate,
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{Deployment: &ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
				},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
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
