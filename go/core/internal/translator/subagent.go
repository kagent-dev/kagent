package translator

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// validateSubAgentIsolation rejects a Session subagent that the selected
// runtime cannot run as intended.
//
// A Session subagent runs as a tool call in a session of its own, inside the
// parent's runtime. Only runtimes that build subagents from the shared ADK
// configuration can do that. The subagent also has no way to reach a person:
// its session ends with the tool call, so a tool that waits for human approval
// would hang the call.
func validateSubAgentIsolation(runtime HarnessType, name string, isolation v1alpha3.SubAgentIsolation, child *AgentInput) error {
	if isolation != v1alpha3.SubAgentIsolationSession {
		return nil
	}
	if runtime != HarnessTypeKagent && runtime != HarnessTypeBYO {
		return NewValidationError("subagent %q runs in its own Session, which Harness runtime %q does not support", name, runtime)
	}
	for _, tool := range child.MCPTools {
		if tool.Binding.RequireApproval {
			return NewValidationError("subagent %q runs in its own Session and cannot pause for human approval, but its binding to %s %q requires it",
				name, tool.Binding.Server.GroupKind().Kind, tool.Binding.Server.Name)
		}
	}
	return nil
}
