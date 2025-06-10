package client

func (c *client) ListSupportedModels() (*ProviderModels, error) {
	var models ProviderModels
	err := c.doRequest("GET", "/models", nil, &models)
	return &models, err
}
