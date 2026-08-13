package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ModelConfigInterface defines the model configuration operations
type ModelConfigInterface interface {
	ListModelConfigs(ctx context.Context) (*api.StandardResponse[[]api.ModelConfigResource], error)
	GetModelConfig(ctx context.Context, namespace, name string) (*api.StandardResponse[*api.ModelConfigResource], error)
	CreateModelConfig(ctx context.Context, request *api.CreateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResource], error)
	UpdateModelConfig(ctx context.Context, namespace, name string, request *api.UpdateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResource], error)
	DeleteModelConfig(ctx context.Context, namespace, name string) error
}

// ModelConfigClient handles model configuration requests
type ModelConfigClient struct {
	client *BaseClient
}

// NewModelConfigClient creates a new model config client
func NewModelConfigClient(client *BaseClient) ModelConfigInterface {
	return &ModelConfigClient{client: client}
}

// ListModelConfigs lists all model configurations
func (c *ModelConfigClient) ListModelConfigs(ctx context.Context) (*api.StandardResponse[[]api.ModelConfigResource], error) {
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListModelConfigs(callContext, &apiv1alpha1.ListModelConfigsRequest{})
	if err != nil {
		return nil, err
	}

	modelConfigs := make([]api.ModelConfigResource, 0, len(response.GetModelConfigs()))
	for _, modelConfig := range response.GetModelConfigs() {
		resource, err := c.client.decodeModelConfig(modelConfig)
		if err != nil {
			return nil, err
		}
		modelConfigs = append(modelConfigs, *resource)
	}
	result := api.NewResponse(modelConfigs, "Successfully listed ModelConfigs", false)
	return &result, nil
}

// GetModelConfig retrieves a specific model configuration
func (c *ModelConfigClient) GetModelConfig(ctx context.Context, namespace, name string) (*api.StandardResponse[*api.ModelConfigResource], error) {
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.GetModelConfig(callContext, &apiv1alpha1.GetModelConfigRequest{
		Ref: namespacedModelConfigRef(namespace, name),
	})
	if err != nil {
		return nil, err
	}
	resource, err := c.client.decodeModelConfig(response.GetModelConfig())
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(resource, "Successfully retrieved ModelConfig", false)
	return &result, nil
}

// CreateModelConfig creates a new model configuration
func (c *ModelConfigClient) CreateModelConfig(ctx context.Context, request *api.CreateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResource], error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "ModelConfig request is required")
	}
	ref, err := createModelConfigRef(request.Ref)
	if err != nil {
		return nil, err
	}
	resource, err := c.client.encodeModelConfig(request.Spec)
	if err != nil {
		return nil, err
	}
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.CreateModelConfig(callContext, &apiv1alpha1.CreateModelConfigRequest{
		Ref:      ref,
		Resource: resource,
		ApiKey:   request.APIKey,
		Secrets:  modelConfigSecrets(request.Secrets),
	})
	if err != nil {
		return nil, err
	}
	created, err := c.client.decodeModelConfig(response.GetModelConfig())
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(created, "Successfully created ModelConfig", false)
	return &result, nil
}

// UpdateModelConfig updates an existing model configuration
func (c *ModelConfigClient) UpdateModelConfig(ctx context.Context, namespace, configName string, request *api.UpdateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResource], error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "ModelConfig request is required")
	}
	resource, err := c.client.encodeModelConfig(request.Spec)
	if err != nil {
		return nil, err
	}
	client, err := c.client.modelServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.UpdateModelConfig(callContext, &apiv1alpha1.UpdateModelConfigRequest{
		Ref:      namespacedModelConfigRef(namespace, configName),
		Resource: resource,
		ApiKey:   request.APIKey,
		Secrets:  modelConfigSecrets(request.Secrets),
	})
	if err != nil {
		return nil, err
	}
	updated, err := c.client.decodeModelConfig(response.GetModelConfig())
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(updated, "Successfully updated ModelConfig", false)
	return &result, nil
}

// DeleteModelConfig deletes a model configuration
func (c *ModelConfigClient) DeleteModelConfig(ctx context.Context, namespace, configName string) error {
	client, err := c.client.modelServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	_, err = client.DeleteModelConfig(callContext, &apiv1alpha1.DeleteModelConfigRequest{
		Ref: namespacedModelConfigRef(namespace, configName),
	})
	return err
}
