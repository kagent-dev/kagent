package driver

import (
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
)

func (b *ApprovalBroker) toolPolicy(toolName string) (config.MCPServer, string, bool) {
	var selected string
	for server := range b.policies {
		prefix := "mcp__" + server + "__"
		if len(server) > len(selected) && strings.HasPrefix(toolName, prefix) && len(toolName) > len(prefix) {
			selected = server
		}
	}
	if selected == "" {
		return config.MCPServer{}, "", false
	}
	return b.policies[selected], strings.TrimPrefix(toolName, "mcp__"+selected+"__"), true
}

func (b *ApprovalBroker) permits(toolName string) bool {
	if !strings.HasPrefix(toolName, "mcp__") || toolName == "mcp__kagent_hitl__"+ApprovalToolName {
		return true
	}
	policy, name, known := b.toolPolicy(toolName)
	return known && (len(policy.Tools) == 0 || slices.Contains(policy.Tools, name))
}
