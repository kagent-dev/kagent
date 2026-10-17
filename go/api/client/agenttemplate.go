package client

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// AgentTemplateClient provides supported AgentTemplate operations.
type AgentTemplateClient struct {
	client *baseClient
}

// newAgentTemplateClient creates an AgentTemplate client over the shared gRPC connection.
func newAgentTemplateClient(client *baseClient) *AgentTemplateClient {
	return &AgentTemplateClient{client: client}
}

func (c *AgentTemplateClient) ListAgentTemplates(ctx context.Context, request *apiv1.ListAgentTemplatesRequest) (*apiv1.ListAgentTemplatesResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListAgentTemplates(callContext, request)
}

func (c *AgentTemplateClient) GetAgentTemplate(ctx context.Context, request *apiv1.GetAgentTemplateRequest) (*apiv1.GetAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.GetAgentTemplate(callContext, request)
}

func (c *AgentTemplateClient) CreateAgentTemplate(ctx context.Context, request *apiv1.CreateAgentTemplateRequest) (*apiv1.CreateAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateAgentTemplate(callContext, request)
}

func (c *AgentTemplateClient) UpdateAgentTemplate(ctx context.Context, request *apiv1.UpdateAgentTemplateRequest) (*apiv1.UpdateAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.UpdateAgentTemplate(callContext, request)
}

func (c *baseClient) agentTemplateCall(ctx context.Context) (apiv1.AgentTemplateServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1.NewAgentTemplateServiceClient(connection), callContext, cancel, nil
}
