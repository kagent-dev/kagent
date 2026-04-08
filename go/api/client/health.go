package client

import (
	"context"
)

// Health defines the health-related operations
type Health interface {
	Get(ctx context.Context) error
}

// healthClient handles health-related requests
type healthClient struct {
	client *BaseClient
}

// NewHealthClient creates a new health client
func NewHealthClient(client *BaseClient) Health {
	return &healthClient{client: client}
}

// Health checks if the server is healthy
func (c *healthClient) Get(ctx context.Context) error {
	_, err := c.client.Get(ctx, "/health", "")
	if err != nil {
		return err
	}
	return nil
}
