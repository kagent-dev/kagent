package client

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Provider defines the provider operations
type Provider interface {
	ListSupportedModelProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error)
	ListSupportedMemoryProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error)
}

// providerClient handles provider-related requests
type providerClient struct {
	client *BaseClient
}

// NewProviderClient creates a new provider client
func NewProviderClient(client *BaseClient) Provider {
	return &providerClient{client: client}
}

// ListSupportedModelProviders lists all supported model providers
func (c *providerClient) ListSupportedModelProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error) {
	resp, err := c.client.Get(ctx, "/api/providers/models", "")
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
func (c *providerClient) ListSupportedMemoryProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error) {
	resp, err := c.client.Get(ctx, "/api/providers/memories", "")
	if err != nil {
		return nil, err
	}

	var providers api.StandardResponse[[]api.ProviderInfo]
	if err := DecodeResponse(resp, &providers); err != nil {
		return nil, err
	}

	return &providers, nil
}
