package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// ToolServer defines the tool server operations
type ToolServer interface {
	ListToolServers(ctx context.Context) ([]api.ToolServerResponse, error)
	CreateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*v1alpha1.ToolServer, error)
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
	resp, err := c.client.Get(ctx, "/api/toolservers", "")
	if err != nil {
		return nil, err
	}

	var toolServers []api.ToolServerResponse
	if err := DecodeResponse(resp, &toolServers); err != nil {
		return nil, err
	}

	return toolServers, nil
}

// CreateToolServer creates a new tool server
func (c *ToolServerClient) CreateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*v1alpha1.ToolServer, error) {
	resp, err := c.client.Post(ctx, "/api/toolservers", toolServer, "")
	if err != nil {
		return nil, err
	}

	var createdToolServer v1alpha1.ToolServer
	if err := DecodeResponse(resp, &createdToolServer); err != nil {
		return nil, err
	}

	return &createdToolServer, nil
}

// DeleteToolServer deletes a tool server
func (c *ToolServerClient) DeleteToolServer(ctx context.Context, namespace, toolServerName string) error {
	path := fmt.Sprintf("/api/toolservers/%s/%s", namespace, toolServerName)
	resp, err := c.client.Delete(ctx, path, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
