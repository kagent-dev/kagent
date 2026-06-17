package v1alpha2

import "fmt"

const (
	substrateSandboxSkillsUnsupportedMsg = "spec.skills is not supported when spec.platform is substrate"
	substrateSandboxBYOMissingCommandMsg = "BYO agents on substrate must set spec.byo.deployment.cmd (substrate does not fall back to the image entrypoint)"
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
// does not support yet (for example declarative skills on Agent Substrate). Declarative
// Python/Go and BYO (Go/Python) agents are supported; BYO agents must provide an explicit
// command because substrate copies the container Command verbatim with no image-entrypoint
// fallback.
func ValidateSubstrateSandboxAgentSpec(agent *SandboxAgent) error {
	if agent == nil || AgentSandboxPlatform(agent) != SandboxPlatformSubstrate {
		return nil
	}
	spec := agent.GetAgentSpec()
	if AgentSpecHasSkills(spec) {
		return fmt.Errorf("%s", substrateSandboxSkillsUnsupportedMsg)
	}
	if spec.Type == AgentType_BYO {
		dep := spec.BYO
		if dep == nil || dep.Deployment == nil || dep.Deployment.Cmd == nil || *dep.Deployment.Cmd == "" {
			return fmt.Errorf("%s", substrateSandboxBYOMissingCommandMsg)
		}
	}
	return nil
}
