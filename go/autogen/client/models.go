package client

import "context"

func (c *client) ListSupportedModels() (*ProviderModels, error) {
	var models ProviderModels
	err := c.doRequest(context.Background(), "GET", "/models", nil, &models)
	return &models, err
}
