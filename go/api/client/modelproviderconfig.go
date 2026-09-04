package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
)

// ConfiguredProvider describes a model provider configured in the cluster.
type ConfiguredProvider struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

// ProviderModelsResult contains discovered models for one configured provider.
type ProviderModelsResult struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// ModelProviderConfig defines the model provider config operations
type ModelProviderConfig interface {
	ListSupportedModelProviders(ctx context.Context) (*api.StandardResponse[[]api.ProviderInfo], error)
	ListConfiguredProviders(ctx context.Context) (*api.StandardResponse[[]ConfiguredProvider], error)
	ListProviderModels(ctx context.Context, providerName string, refresh bool) (*api.StandardResponse[ProviderModelsResult], error)
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
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListSupportedModelProviders(callContext, &apiv1alpha1.ListSupportedModelProvidersRequest{})
	if err != nil {
		return nil, err
	}

	providers := make([]api.ProviderInfo, 0, len(response.GetProviders()))
	for _, provider := range response.GetProviders() {
		providers = append(providers, providerInfo(provider))
	}
	result := api.NewResponse(providers, "Successfully listed supported model providers", false)
	return &result, nil
}

// ListConfiguredProviders lists model providers configured in the cluster.
func (c *modelProviderConfigClient) ListConfiguredProviders(ctx context.Context) (*api.StandardResponse[[]ConfiguredProvider], error) {
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListConfiguredProviders(callContext, &apiv1alpha1.ListConfiguredProvidersRequest{})
	if err != nil {
		return nil, err
	}

	providers := make([]ConfiguredProvider, 0, len(response.GetProviders()))
	for _, provider := range response.GetProviders() {
		providers = append(providers, ConfiguredProvider{
			Name:     provider.GetName(),
			Type:     provider.GetType(),
			Endpoint: provider.GetEndpoint(),
		})
	}
	result := api.NewResponse(providers, "Successfully listed configured model providers", false)
	return &result, nil
}

// ListProviderModels returns cached or freshly discovered models for a provider.
func (c *modelProviderConfigClient) ListProviderModels(ctx context.Context, providerName string, refresh bool) (*api.StandardResponse[ProviderModelsResult], error) {
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListProviderModels(callContext, &apiv1alpha1.ListProviderModelsRequest{
		ProviderName: providerName,
		Refresh:      refresh,
	})
	if err != nil {
		return nil, err
	}

	models := ProviderModelsResult{
		Provider: response.GetProvider(),
		Models:   append([]string(nil), response.GetModels()...),
	}
	result := api.NewResponse(models, "Successfully retrieved models", false)
	return &result, nil
}
