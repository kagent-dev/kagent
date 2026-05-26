package client

import (
	"context"
	"fmt"
	"net/url"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// Agent defines the agent operations
type Agent interface {
	ListAgents(ctx context.Context, opts ...ListAgentsOptions) (*api.StandardResponse[[]api.AgentResponse], error)
	CreateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error)
	GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error)
	UpdateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error)
	DeleteAgent(ctx context.Context, agentRef string) error
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

// CreateAgent creates a new agent
func (c *agentClient) CreateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error) {
	resp, err := c.client.Post(ctx, "/api/agents", request, "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*v1alpha2.Agent]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetAgent retrieves a specific agent
func (c *agentClient) GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error) {
	path := fmt.Sprintf("/api/agents/%s", agentRef)
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

// UpdateAgent updates an existing agent
func (c *agentClient) UpdateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error) {
	path := fmt.Sprintf("/api/agents/%s/%s", request.Namespace, request.Name)
	resp, err := c.client.Put(ctx, path, request, "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*v1alpha2.Agent]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// DeleteAgent deletes a agent
func (c *agentClient) DeleteAgent(ctx context.Context, agentRef string) error {
	path := fmt.Sprintf("/api/agents/%s", agentRef)
	resp, err := c.client.Delete(ctx, path, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
