package a2a

import (
	"context"
	"fmt"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
)

// AgentClientRegistry maps agent route keys to their A2A clients.
// The A2ARegistrar populates it; the MCP handler reads from it to invoke
// agents without an HTTP round trip through the controller's own A2A listener.
type AgentClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*a2aclient.Client
}

func NewAgentClientRegistry() *AgentClientRegistry {
	return &AgentClientRegistry{clients: make(map[string]*a2aclient.Client)}
}

func (r *AgentClientRegistry) set(agentRef string, c *a2aclient.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[agentRef] = c
}

func (r *AgentClientRegistry) delete(agentRef string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, agentRef)
}

// Register adds or replaces the A2A client for the given agent.
func (r *AgentClientRegistry) Register(namespace, name string, c *a2aclient.Client) {
	r.set(namespace+"/"+name, c)
}

// SendMessage invokes an agent directly via its cached A2A client.
func (r *AgentClientRegistry) SendMessage(ctx context.Context, namespace, name string, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	key := namespace + "/" + name
	r.mu.RLock()
	c, ok := r.clients[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent %s/%s not found or not ready", namespace, name)
	}
	return c.SendMessage(ctx, req)
}
