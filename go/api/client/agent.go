package client

import (
	"context"
	"fmt"
	"net/url"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
)

// Agent defines the agent operations
type Agent interface {
	ListAgents(ctx context.Context, opts ...ListAgentsOptions) (*api.StandardResponse[[]api.AgentResponse], error)
	GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error)
}

// ListAgentsOptions configures ListAgents requests.
type ListAgentsOptions struct {
	Namespace string
}

// agentClient handles agent-related requests
type agentClient struct {
	client *BaseClient
}

// NewAgentClient creates a new agent client
func NewAgentClient(client *BaseClient) Agent {
	return &agentClient{client: client}
}

// ListAgents lists all agents for a user. When Namespace is set, only agents in that namespace are returned.
func (c *agentClient) ListAgents(ctx context.Context, opts ...ListAgentsOptions) (*api.StandardResponse[[]api.AgentResponse], error) {
	if len(opts) > 1 {
		return nil, fmt.Errorf("ListAgents accepts at most one options argument")
	}

	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	path := "/api/agents"
	if len(opts) > 0 && opts[0].Namespace != "" {
		path += "?namespace=" + url.QueryEscape(opts[0].Namespace)
	}

	resp, err := c.client.Get(ctx, path, userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[[]api.AgentResponse]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetAgent retrieves a specific agent
func (c *agentClient) GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error) {
	list, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	kind := ""
	for _, row := range list.Data {
		ns := row.Agent.Metadata.Namespace
		name := row.Agent.Metadata.Name
		ref := fmt.Sprintf("%s/%s", ns, name)
		if ref == agentRef || name == agentRef {
			kind = row.Agent.Kind
			break
		}
	}
	path := fmt.Sprintf("/api/sandboxagents/%s", agentRef)
	switch kind {
	case "AgentHarness":
		path = fmt.Sprintf("/api/agentharnesses/%s", agentRef)
	}
	resp, err := c.client.Get(ctx, path, "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.AgentResponse]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}
