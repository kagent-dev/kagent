// Package a2a defines the public A2A routing contract for AgentInstances.
package a2a

const (
	// AgentInstanceNamespaceHeader selects the namespace scope for the AgentInstance.
	AgentInstanceNamespaceHeader = "x-kagent-agent-instance-namespace"
	// AgentInstanceIDHeader selects the AgentInstance within that namespace scope.
	AgentInstanceIDHeader = "x-kagent-agent-instance-id"
)
