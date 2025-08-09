package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Agent defines the agent operations
type Agent interface {
	ListAgents(ctx context.Context, userID string) (*api.StandardResponse[[]api.AgentResponse], error)
	CreateAgent(ctx context.Context, request *api.CreateAgentRequest) (*api.StandardResponse[*api.AgentResponse], error)
	GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error)
	UpdateAgent(ctx context.Context, namespace, name string, request *api.UpdateAgentRequest) (*api.StandardResponse[*api.AgentResponse], error)
	DeleteAgent(ctx context.Context, agentRef string) error
}

// agentClient handles agent-related requests
type agentClient struct {
	client *BaseClient
}

// NewAgentClient creates a new agent client
func NewAgentClient(client *BaseClient) Agent {
	return &agentClient{client: client}
}

// ListTeams lists all agents for a user
func (c *agentClient) ListAgents(ctx context.Context, userID string) (*api.StandardResponse[[]api.AgentResponse], error) {
	userID = c.client.GetUserIDOrDefault(userID)
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	resp, err := c.client.Get(ctx, "/api/agents", userID)
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
func (c *agentClient) CreateAgent(ctx context.Context, request *api.CreateAgentRequest) (*api.StandardResponse[*api.AgentResponse], error) {
	resp, err := c.client.Post(ctx, "/api/agents", request, "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.AgentResponse]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetTeam retrieves a specific agent
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
func (c *agentClient) UpdateAgent(ctx context.Context, namespace, name string, request *api.UpdateAgentRequest) (*api.StandardResponse[*api.AgentResponse], error) {
	path := fmt.Sprintf("/api/agents/%s/%s", namespace, name)
	resp, err := c.client.Put(ctx, path, request, "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.AgentResponse]
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
