package v1alpha2

import (
	"fmt"
	"strings"
)

const (
	substrateSandboxSkillsUnsupportedMsg = "spec.skills is not supported for sandbox agents"
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

// ValidateSubstrateSandboxAgentSpec rejects sandbox agent configurations that kagent
// does not support on Agent Substrate (for example declarative skills). Declarative
// Python/Go and BYO (Go/Python) agents are supported; BYO agents must provide an explicit
// command because substrate copies the container Command verbatim with no image-entrypoint
// fallback.
func ValidateSubstrateSandboxAgentSpec(agent *SandboxAgent) error {
	if agent == nil {
		return nil
	}
	spec := agent.GetAgentSpec()
	if AgentSpecHasSkills(spec) {
		return fmt.Errorf("%s", substrateSandboxSkillsUnsupportedMsg)
	}
	if spec.Type == AgentType_BYO {
		dep := spec.BYO
		// Trim so a whitespace-only cmd is rejected like an empty one (substrate would treat it
		// as no command, and the UI trims before validating — keep backend/UI aligned).
		if dep == nil || dep.Deployment == nil || dep.Deployment.Cmd == nil || strings.TrimSpace(*dep.Deployment.Cmd) == "" {
			return fmt.Errorf("%s", substrateSandboxBYOMissingCommandMsg)
		}
	}
	return nil
}
