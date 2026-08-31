package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// AgentTemplateClient provides supported AgentTemplate operations.
type AgentTemplateClient struct {
	client *BaseClient
}

// NewAgentTemplateClient creates an AgentTemplate client over the shared gRPC connection.
func NewAgentTemplateClient(client *BaseClient) *AgentTemplateClient {
	return &AgentTemplateClient{client: client}
}

func (c *AgentTemplateClient) CreateAgentTemplate(ctx context.Context, request *apiv1alpha1.CreateAgentTemplateRequest) (*apiv1alpha1.CreateAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateAgentTemplate(callContext, request)
}

func (c *AgentTemplateClient) UpdateAgentTemplate(ctx context.Context, request *apiv1alpha1.UpdateAgentTemplateRequest) (*apiv1alpha1.UpdateAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.UpdateAgentTemplate(callContext, request)
}

func (c *AgentTemplateClient) DeleteAgentTemplate(ctx context.Context, request *apiv1alpha1.DeleteAgentTemplateRequest) (*apiv1alpha1.DeleteAgentTemplateResponse, error) {
	client, callContext, cancel, err := c.client.agentTemplateCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.DeleteAgentTemplate(callContext, request)
}

func (c *BaseClient) agentTemplateCall(ctx context.Context) (apiv1alpha1.AgentTemplateServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1alpha1.NewAgentTemplateServiceClient(connection), callContext, cancel, nil
}
