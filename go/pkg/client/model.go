package client

import (
	"context"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// ModelInfo represents information about a model
type ModelInfo struct {
	Name            string `json:"name"`
	FunctionCalling bool   `json:"function_calling"`
}

// ProviderModels represents a map of provider names to their supported models
type ProviderModels map[v1alpha2.ModelProvider][]ModelInfo

// Model defines the model operations
type Model interface {
	ListSupportedModels(ctx context.Context) (*api.StandardResponse[ProviderModels], error)
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
func (c *modelClient) ListSupportedModels(ctx context.Context) (*api.StandardResponse[ProviderModels], error) {
	resp, err := c.client.Get(ctx, "/api/models", "")
	if err != nil {
		return nil, err
	}

	var models api.StandardResponse[ProviderModels]
	if err := DecodeResponse(resp, &models); err != nil {
		return nil, err
	}

	return &models, nil
}
