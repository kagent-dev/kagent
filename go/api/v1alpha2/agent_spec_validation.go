package v1alpha2

import (
	"fmt"
	"strings"
)

const (
	substrateSandboxSkillsUnsupportedMsg       = "spec.skills is not supported for sandbox agents"
	substrateSandboxBYOMissingCommandMsg       = "BYO agents on substrate must set spec.byo.deployment.cmd (substrate does not fall back to the image entrypoint)"
	substrateSandboxNodeSelectorUnsupportedMsg = "deployment.nodeSelector is not supported for sandbox agents: substrate schedules actors onto WorkerPool workers, so set the WorkerPool's nodeSelector instead"
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
// fallback. A per-agent deployment.nodeSelector is rejected: substrate ActorTemplates carry
// no node placement (actors run on WorkerPool workers), so the selector would otherwise be
// silently dropped. The skills and nodeSelector checks are also enforced at admission by CEL
// rules on SandboxAgentSpec; this function keeps them effective for objects created before
// those rules shipped and for callers that bypass the API server.
func ValidateSubstrateSandboxAgentSpec(agent *SandboxAgent) error {
	if agent == nil {
		return nil
	}
	spec := agent.GetAgentSpec()
	if AgentSpecHasSkills(spec) {
		return fmt.Errorf("%s", substrateSandboxSkillsUnsupportedMsg)
	}
	if len(agentSpecNodeSelector(spec)) > 0 {
		return fmt.Errorf("%s", substrateSandboxNodeSelectorUnsupportedMsg)
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

// agentSpecNodeSelector returns the per-agent deployment nodeSelector, whichever agent
// type carries it.
func agentSpecNodeSelector(spec *AgentSpec) map[string]string {
	if spec == nil {
		return nil
	}
	if spec.Declarative != nil && spec.Declarative.Deployment != nil {
		return spec.Declarative.Deployment.NodeSelector
	}
	if spec.BYO != nil && spec.BYO.Deployment != nil {
		return spec.BYO.Deployment.NodeSelector
	}
	return nil
}
