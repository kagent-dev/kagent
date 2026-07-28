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

	t.Run("allows python runtime", func(t *testing.T) {
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
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("rejects BYO agents without an explicit command", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
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

	t.Run("rejects BYO agents with a whitespace-only command", func(t *testing.T) {
		cmd := "   "
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
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

	t.Run("allows BYO agents with an explicit command", func(t *testing.T) {
		cmd := "/app"
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO:  &BYOAgentSpec{Deployment: &ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
				},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
	})

	t.Run("rejects declarative agents with a nodeSelector", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				AgentSpec: AgentSpec{
					Type: AgentType_Declarative,
					Declarative: &DeclarativeAgentSpec{
						Runtime: DeclarativeRuntime_Go,
						Deployment: &DeclarativeDeploymentSpec{
							SharedDeploymentSpec: SharedDeploymentSpec{
								NodeSelector: map[string]string{"kubernetes.io/arch": "amd64", "topology.kubernetes.io/zone": "z1"},
							},
						},
					},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxNodeSelectorUnsupportedMsg)
	})

	t.Run("rejects BYO agents with a nodeSelector", func(t *testing.T) {
		cmd := "/app"
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				AgentSpec: AgentSpec{
					Type: AgentType_BYO,
					BYO: &BYOAgentSpec{Deployment: &ByoDeploymentSpec{
						Image: "example/agent:latest",
						Cmd:   &cmd,
						SharedDeploymentSpec: SharedDeploymentSpec{
							NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"},
						},
					}},
				},
			},
		}
		err := ValidateSubstrateSandboxAgentSpec(agent)
		require.Error(t, err)
		require.Contains(t, err.Error(), substrateSandboxNodeSelectorUnsupportedMsg)
	})

	t.Run("allows declarative agents with an empty nodeSelector", func(t *testing.T) {
		agent := &SandboxAgent{
			Spec: SandboxAgentSpec{
				AgentSpec: AgentSpec{
					Type: AgentType_Declarative,
					Declarative: &DeclarativeAgentSpec{
						Runtime: DeclarativeRuntime_Go,
						Deployment: &DeclarativeDeploymentSpec{
							SharedDeploymentSpec: SharedDeploymentSpec{
								NodeSelector: map[string]string{},
							},
						},
					},
				},
			},
		}
		require.NoError(t, ValidateSubstrateSandboxAgentSpec(agent))
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
