package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ModelInfo represents information about a model
type ModelInfo struct {
	Name            string `json:"name"`
	FunctionCalling bool   `json:"function_calling"`
}

// ProviderModels represents a map of provider names to their supported models
type ProviderModels map[v1alpha3.ModelProvider][]ModelInfo

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
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListSupportedModels(callContext, &apiv1alpha1.ListSupportedModelsRequest{})
	if err != nil {
		return nil, err
	}

	models := make(ProviderModels, len(response.GetProviders()))
	for _, provider := range response.GetProviders() {
		providerModels := make([]ModelInfo, 0, len(provider.GetModels()))
		for _, model := range provider.GetModels() {
			providerModels = append(providerModels, ModelInfo{
				Name:            model.GetName(),
				FunctionCalling: model.GetFunctionCalling(),
			})
		}
		models[v1alpha3.ModelProvider(provider.GetProvider())] = providerModels
	}
	result := api.NewResponse(models, "Successfully listed supported models", false)
	return &result, nil
}
