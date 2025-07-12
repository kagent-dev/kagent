package client

import (
	"context"

	"github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Model defines the model operations
type Model interface {
	ListSupportedModels(ctx context.Context) (*api.StandardResponse[*client.ProviderModels], error)
}

// modelClient handles model-related requests
type modelClient struct {
	client *BaseClient
}

// NewModelClient creates a new model client
func NewModelClient(client *BaseClient) Model {
	return &modelClient{client: client}
}

// ListSupportedModels lists all supported models
func (c *modelClient) ListSupportedModels(ctx context.Context) (*api.StandardResponse[*client.ProviderModels], error) {
	resp, err := c.client.Get(ctx, "/api/models", "")
	if err != nil {
		return nil, err
	}

	var models api.StandardResponse[*client.ProviderModels]
	if err := DecodeResponse(resp, &models); err != nil {
		return nil, err
	}

	return &models, nil
}
