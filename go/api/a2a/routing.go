// Package a2a defines kagent's public A2A routing contract.
package a2a

const (
	// AgentInstanceNamespaceHeader selects the Kubernetes namespace containing the AgentInstance.
	AgentInstanceNamespaceHeader = "x-kagent-agent-instance-namespace"
	// AgentInstanceIDHeader selects the AgentInstance within that namespace.
	AgentInstanceIDHeader = "x-kagent-agent-instance-id"
)
