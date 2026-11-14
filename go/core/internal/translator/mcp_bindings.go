package translator

import (
	"fmt"
	"slices"
)

// MCPBindingNames gives each approval group a native identity with an exact tool scope.
func MCPBindingNames(namespace string, tools []ResolvedMCPTool) ([]string, error) {
	names := make([]string, len(tools))
	for i, tool := range tools {
		if tool.Server == nil {
			return nil, fmt.Errorf("resolved MCP binding has no server")
		}
		name := MCPRuntimeName(namespace, tool.Server)
		if tool.Binding.RequireApproval {
			name += "-approval"
		}
		names[i] = name
		for _, previous := range tools[:i] {
			if previous.Server.Namespace != tool.Server.Namespace || previous.Server.Name != tool.Server.Name ||
				previous.Binding.RequireApproval == tool.Binding.RequireApproval {
				continue
			}
			if len(previous.Binding.Tools) == 0 || len(tool.Binding.Tools) == 0 {
				return nil, NewValidationError("mixed approval groups for RemoteMCPServer %q require explicit tool selections", tool.Server.Name)
			}
			for _, selected := range tool.Binding.Tools {
				if slices.Contains(previous.Binding.Tools, selected) {
					return nil, NewValidationError("RemoteMCPServer %q tool %q has conflicting approval requirements", tool.Server.Name, selected)
				}
			}
		}
	}
	return names, nil
}
