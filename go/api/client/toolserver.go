package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToolServer defines the tool server operations
type ToolServer interface {
	ListToolServers(ctx context.Context) ([]api.ToolServerResponse, error)
	DeleteToolServer(ctx context.Context, namespace, toolServerName string) error
}

// ToolServerClient handles tool server-related requests
type ToolServerClient struct {
	client *BaseClient
}

// NewToolServerClient creates a new tool server client
func NewToolServerClient(client *BaseClient) ToolServer {
	return &ToolServerClient{client: client}
}

// ListToolServers lists all tool servers
func (c *ToolServerClient) ListToolServers(ctx context.Context) ([]api.ToolServerResponse, error) {
	client, err := c.client.toolServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListToolServers(callContext, &apiv1alpha1.ListToolServersRequest{})
	if err != nil {
		return nil, err
	}

	toolServers := make([]api.ToolServerResponse, 0, len(response.GetToolServers()))
	for _, message := range response.GetToolServers() {
		discoveredTools := make([]*v1alpha3.MCPTool, 0, len(message.GetDiscoveredTools()))
		for _, tool := range message.GetDiscoveredTools() {
			discoveredTools = append(discoveredTools, &v1alpha3.MCPTool{
				Name:        tool.GetName(),
				Description: tool.GetDescription(),
			})
		}
		toolServers = append(toolServers, api.ToolServerResponse{
			Ref:             message.GetRef(),
			GroupKind:       message.GetGroupKind(),
			DiscoveredTools: discoveredTools,
		})
	}
	return toolServers, nil
}

// DeleteToolServer deletes a tool server
func (c *ToolServerClient) DeleteToolServer(ctx context.Context, namespace, toolServerName string) error {
	if namespace == "" || toolServerName == "" {
		return status.Error(codes.InvalidArgument, "ToolServer namespace and name are required")
	}
	client, err := c.client.toolServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	_, err = client.DeleteToolServer(callContext, &apiv1alpha1.DeleteToolServerRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: namespace, Name: toolServerName},
	})
	return err
}
