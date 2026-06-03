package v1alpha2

import "fmt"

const substrateSandboxSkillsUnsupportedMsg = "spec.skills is not supported when spec.sandbox.platform is substrate"

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
func ValidateSubstrateSandboxAgentSpec(spec *AgentSpec) error {
	if spec == nil || AgentSandboxPlatform(spec) != SandboxPlatformSubstrate {
		return nil
	}
	if AgentSpecHasSkills(spec) {
		return fmt.Errorf("%s", substrateSandboxSkillsUnsupportedMsg)
	}
	return nil
}
