package a2a

import (
	"context"
	"fmt"
	"sync"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// AgentClientRegistry maps agent route keys to their A2A clients.
// The A2ARegistrar populates it; the MCP handler reads from it to invoke
// agents without an HTTP round trip through the controller's own A2A listener.
type AgentClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*a2aclient.A2AClient
}

func NewAgentClientRegistry() *AgentClientRegistry {
	return &AgentClientRegistry{clients: make(map[string]*a2aclient.A2AClient)}
}

// set stores the client under the agent's route key (e.g. "namespace/name" or
// "sandboxes/namespace/name").
func (r *AgentClientRegistry) set(agentRef string, c *a2aclient.A2AClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[agentRef] = c
}

// delete removes the client for the given agent route key.
func (r *AgentClientRegistry) delete(agentRef string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, agentRef)
}

// Register adds or replaces the A2A client for the given agent. It is the
// exported counterpart of set, intended for use in tests and explicit
// registrations outside the A2ARegistrar lifecycle.
func (r *AgentClientRegistry) Register(namespace, name string, c *a2aclient.A2AClient) {
	r.set(namespace+"/"+name, c)
}

// SendMessage invokes an agent directly via its cached A2A client.
// namespace and name must identify a non-sandbox agent; sandbox agents use a
// different route key and are not yet reachable via this method.
func (r *AgentClientRegistry) SendMessage(ctx context.Context, namespace, name string, params protocol.SendMessageParams) (*protocol.MessageResult, error) {
	key := namespace + "/" + name
	r.mu.RLock()
	c, ok := r.clients[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent %s/%s not found or not ready", namespace, name)
	}
	return c.SendMessage(ctx, params)
}
