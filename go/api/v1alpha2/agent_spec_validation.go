package v1alpha2

import "fmt"

const (
	substrateSandboxSkillsUnsupportedMsg        = "spec.skills is not supported when spec.platform is substrate"
	substrateSandboxPythonRuntimeUnsupportedMsg = "spec.declarative.runtime must be \"go\" when spec.platform is substrate"
	substrateSandboxBYOUnsupportedMsg           = "BYO agents are not supported when spec.platform is substrate"
)

// AgentSpecHasSkills reports whether the spec configures any skill sources.
func AgentSpecHasSkills(spec *AgentSpec) bool {
	if spec == nil || spec.Skills == nil {
		return false
	}
	s := spec.Skills
	return len(s.Refs) > 0 || len(s.GitRefs) > 0
}

// ValidateSubstrateSandboxAgentSpec rejects substrate sandbox configurations that kagent
// does not support yet (for example declarative skills on Agent Substrate).
func ValidateSubstrateSandboxAgentSpec(agent *SandboxAgent) error {
	if agent == nil || AgentSandboxPlatform(agent) != SandboxPlatformSubstrate {
		return nil
	}
	spec := agent.GetAgentSpec()
	if spec.Type == AgentType_BYO {
		return fmt.Errorf("%s", substrateSandboxBYOUnsupportedMsg)
	}
	if AgentSpecHasSkills(spec) {
		return fmt.Errorf("%s", substrateSandboxSkillsUnsupportedMsg)
	}
	if spec.Type == AgentType_Declarative &&
		spec.Declarative != nil &&
		spec.Declarative.Runtime != "" &&
		spec.Declarative.Runtime != DeclarativeRuntime_Go {
		return fmt.Errorf("%s", substrateSandboxPythonRuntimeUnsupportedMsg)
	}
	return nil
}
