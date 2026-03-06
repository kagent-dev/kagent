package temporal

import "strings"

// ParseWorkflowID extracts agent name and session ID from a workflow ID
// following the pattern "agent-{agentName}-{sessionID}".
// Returns empty strings if the pattern doesn't match.
func ParseWorkflowID(id string) (agentName, sessionID string) {
	if !strings.HasPrefix(id, "agent-") {
		return "", ""
	}
	rest := strings.TrimPrefix(id, "agent-")
	// Find the last hyphen to split agent name from session ID.
	// Agent names may contain hyphens (e.g., "k8s-agent"), so we take
	// the last segment as session ID.
	idx := strings.LastIndex(rest, "-")
	if idx < 0 {
		return rest, ""
	}
	return rest[:idx], rest[idx+1:]
}
