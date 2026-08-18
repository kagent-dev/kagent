package client

import (
	"context"
	"fmt"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
)

const clientToolKind = "Tool"

// Tool defines the tool operations
type Tool interface {
	ListTools(ctx context.Context) ([]api.Tool, error)
}

// toolClient handles tool-related requests
type toolClient struct {
	client *BaseClient
}

// NewToolClient creates a new tool client
func NewToolClient(client *BaseClient) Tool {
	return &toolClient{client: client}
}

// ListTools lists all tools for a user
func (c *toolClient) ListTools(ctx context.Context) ([]api.Tool, error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	client, err := c.client.toolServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListTools(callContext, &apiv1alpha1.ListToolsRequest{})
	if err != nil {
		return nil, err
	}

	tools := make([]api.Tool, 0, len(response.GetTools()))
	for _, message := range response.GetTools() {
		var tool api.Tool
		if err := structuredobject.ToGo(message.GetResource(), clientToolKind, &tool, c.client.grpc.maxMessageBytes); err != nil {
			return nil, fmt.Errorf("decode Tool resource: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (c *BaseClient) toolServiceClient() (apiv1alpha1.ToolServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewToolServiceClient(connection), nil
}
