package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// AgentInstanceClient provides supported AgentInstance operations.
type AgentInstanceClient struct {
	client *BaseClient
}

// NewAgentInstanceClient creates an AgentInstance client over the shared gRPC connection.
func NewAgentInstanceClient(client *BaseClient) *AgentInstanceClient {
	return &AgentInstanceClient{client: client}
}

func (c *AgentInstanceClient) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateAgentInstance(callContext, request)
}

func (c *AgentInstanceClient) GetAgentInstance(ctx context.Context, request *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.GetAgentInstance(callContext, request)
}

func (c *AgentInstanceClient) ListAgentInstances(ctx context.Context, request *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListAgentInstances(callContext, request)
}

func (c *AgentInstanceClient) DeleteAgentInstance(ctx context.Context, request *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.DeleteAgentInstance(callContext, request)
}

func (c *BaseClient) agentInstanceCall(ctx context.Context) (apiv1alpha1.AgentInstanceServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection(c.api)
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1alpha1.NewAgentInstanceServiceClient(connection), callContext, cancel, nil
}
