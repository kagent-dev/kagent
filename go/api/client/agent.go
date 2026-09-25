package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// AgentClient provides supported Agent operations.
type AgentClient struct {
	client *baseClient
}

// newAgentClient creates an Agent client over the shared gRPC connection.
func newAgentClient(client *baseClient) *AgentClient {
	return &AgentClient{client: client}
}

func (c *AgentClient) CreateAgent(ctx context.Context, request *apiv1alpha1.CreateAgentRequest) (*apiv1alpha1.CreateAgentResponse, error) {
	client, callContext, cancel, err := c.client.agentCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateAgent(callContext, request)
}

func (c *AgentClient) UpdateAgent(ctx context.Context, request *apiv1alpha1.UpdateAgentRequest) (*apiv1alpha1.UpdateAgentResponse, error) {
	client, callContext, cancel, err := c.client.agentCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.UpdateAgent(callContext, request)
}

func (c *baseClient) agentCall(ctx context.Context) (apiv1alpha1.AgentServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1alpha1.NewAgentServiceClient(connection), callContext, cancel, nil
}
