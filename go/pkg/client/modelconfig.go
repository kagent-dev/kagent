package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// ModelConfigInterface defines the model configuration operations
type ModelConfigInterface interface {
	ListModelConfigs(ctx context.Context) (*api.StandardResponse[[]api.ModelConfigResponse], error)
	GetModelConfig(ctx context.Context, namespace, name string) (*api.StandardResponse[*api.ModelConfigResponse], error)
	CreateModelConfig(ctx context.Context, request *api.CreateModelConfigRequest) (*api.StandardResponse[*v1alpha1.ModelConfig], error)
	UpdateModelConfig(ctx context.Context, namespace, name string, request *api.UpdateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResponse], error)
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
func (c *ModelConfigClient) ListModelConfigs(ctx context.Context) (*api.StandardResponse[[]api.ModelConfigResponse], error) {
	resp, err := c.client.Get(ctx, "/api/modelconfigs", "")
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[[]api.ModelConfigResponse]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetModelConfig retrieves a specific model configuration
func (c *ModelConfigClient) GetModelConfig(ctx context.Context, namespace, name string) (*api.StandardResponse[*api.ModelConfigResponse], error) {
	path := fmt.Sprintf("/api/modelconfigs/%s/%s", namespace, name)
	resp, err := c.client.Get(ctx, path, "")
	if err != nil {
		return nil, err
	}

	var config api.StandardResponse[*api.ModelConfigResponse]
	if err := DecodeResponse(resp, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// CreateModelConfig creates a new model configuration
func (c *ModelConfigClient) CreateModelConfig(ctx context.Context, request *api.CreateModelConfigRequest) (*api.StandardResponse[*v1alpha1.ModelConfig], error) {
	resp, err := c.client.Post(ctx, "/api/modelconfigs", request, "")
	if err != nil {
		return nil, err
	}

	var config api.StandardResponse[*v1alpha1.ModelConfig]
	if err := DecodeResponse(resp, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// UpdateModelConfig updates an existing model configuration
func (c *ModelConfigClient) UpdateModelConfig(ctx context.Context, namespace, configName string, request *api.UpdateModelConfigRequest) (*api.StandardResponse[*api.ModelConfigResponse], error) {
	path := fmt.Sprintf("/api/modelconfigs/%s/%s", namespace, configName)
	resp, err := c.client.Put(ctx, path, request, "")
	if err != nil {
		return nil, err
	}

	var config api.StandardResponse[*api.ModelConfigResponse]
	if err := DecodeResponse(resp, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// DeleteModelConfig deletes a model configuration
func (c *ModelConfigClient) DeleteModelConfig(ctx context.Context, namespace, configName string) error {
	path := fmt.Sprintf("/api/modelconfigs/%s/%s", namespace, configName)
	_, err := c.client.Delete(ctx, path, "")
	if err != nil {
		return err
	}
	return nil
}
