package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Tool defines the tool operations
type Tool interface {
	ListTools(ctx context.Context, userID string) ([]api.Tool, error)
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
func (c *toolClient) ListTools(ctx context.Context, userID string) ([]api.Tool, error) {
	userID = c.client.GetUserIDOrDefault(userID)
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	resp, err := c.client.Get(ctx, "/api/tools", userID)
	if err != nil {
		return nil, err
	}

	var tools api.StandardResponse[[]api.Tool]
	if err := DecodeResponse(resp, &tools); err != nil {
		return nil, err
	}

	return tools.Data, nil
}
