package client

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// ModelProviderConfig defines the model provider config operations
type ModelProviderConfig interface {
	ListSupportedModelProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error)
	ListSupportedMemoryProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error)
}

// modelProviderConfigClient handles model provider config related requests
type modelProviderConfigClient struct {
	client *BaseClient
}

// NewModelProviderConfigClient creates a new model provider config client
func NewModelProviderConfigClient(client *BaseClient) ModelProviderConfig {
	return &modelProviderConfigClient{client: client}
}

// ListSupportedModelProviders lists all supported model providers
func (c *modelProviderConfigClient) ListSupportedModelProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error) {
	resp, err := c.client.Get(ctx, "/api/modelproviderconfigs/models", "")
	if err != nil {
		return nil, err
	}

	var providers api.StandardResponse[[]api.ProviderInfo]
	if err := DecodeResponse(resp, &providers); err != nil {
		return nil, err
	}

	return &providers, nil
}

// ListSupportedMemoryProviders lists all supported memory providers
func (c *modelProviderConfigClient) ListSupportedMemoryProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error) {
	resp, err := c.client.Get(ctx, "/api/modelproviderconfigs/memories", "")
	if err != nil {
		return nil, err
	}

	var providers api.StandardResponse[[]api.ProviderInfo]
	if err := DecodeResponse(resp, &providers); err != nil {
		return nil, err
	}

	return &providers, nil
}
