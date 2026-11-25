package client

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// ToolClient provides supported MCP tool server operations.
type ToolClient struct {
	client *baseClient
}

// newToolClient creates a tool server client over the shared gRPC connection.
func newToolClient(client *baseClient) *ToolClient {
	return &ToolClient{client: client}
}

func (c *ToolClient) ListToolServers(ctx context.Context, request *apiv1.ListToolServersRequest) (*apiv1.ListToolServersResponse, error) {
	client, callContext, cancel, err := c.client.toolCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListToolServers(callContext, request)
}

func (c *baseClient) toolCall(ctx context.Context) (apiv1.ToolServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1.NewToolServiceClient(connection), callContext, cancel, nil
}
