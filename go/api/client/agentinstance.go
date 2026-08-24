package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// AgentInstance provides AgentInstance lifecycle operations.
type AgentInstance interface {
	CreateAgentInstance(context.Context, *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error)
	GetAgentInstance(context.Context, *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error)
	ListAgentInstances(context.Context, *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error)
	SuspendAgentInstance(context.Context, *apiv1alpha1.SuspendAgentInstanceRequest) (*apiv1alpha1.SuspendAgentInstanceResponse, error)
	ResumeAgentInstance(context.Context, *apiv1alpha1.ResumeAgentInstanceRequest) (*apiv1alpha1.ResumeAgentInstanceResponse, error)
	DeleteAgentInstance(context.Context, *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error)
}

type agentInstanceClient struct {
	client *BaseClient
}

// NewAgentInstanceClient creates an AgentInstance client over the shared gRPC connection.
func NewAgentInstanceClient(client *BaseClient) AgentInstance {
	return &agentInstanceClient{client: client}
}

func (c *agentInstanceClient) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateAgentInstance(callContext, request)
}

func (c *agentInstanceClient) GetAgentInstance(ctx context.Context, request *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.GetAgentInstance(callContext, request)
}

func (c *agentInstanceClient) ListAgentInstances(ctx context.Context, request *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListAgentInstances(callContext, request)
}

func (c *agentInstanceClient) SuspendAgentInstance(ctx context.Context, request *apiv1alpha1.SuspendAgentInstanceRequest) (*apiv1alpha1.SuspendAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.SuspendAgentInstance(callContext, request)
}

func (c *agentInstanceClient) ResumeAgentInstance(ctx context.Context, request *apiv1alpha1.ResumeAgentInstanceRequest) (*apiv1alpha1.ResumeAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ResumeAgentInstance(callContext, request)
}

func (c *agentInstanceClient) DeleteAgentInstance(ctx context.Context, request *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error) {
	client, callContext, cancel, err := c.client.agentInstanceCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.DeleteAgentInstance(callContext, request)
}

func (c *BaseClient) agentInstanceCall(ctx context.Context) (apiv1alpha1.AgentInstanceServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1alpha1.NewAgentInstanceServiceClient(connection), callContext, cancel, nil
}
