package client

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Version defines the version-related operations
type Version interface {
	GetVersion(ctx context.Context) (*api.VersionResponse, error)
}

// versionClient handles version-related requests
type versionClient struct {
	client *BaseClient
}

// NewVersionClient creates a new version client
func NewVersionClient(client *BaseClient) Version {
	return &versionClient{client: client}
}

// GetVersion retrieves version information
func (c *versionClient) GetVersion(ctx context.Context) (*api.VersionResponse, error) {
	resp, err := c.client.Get(ctx, "/version", "")
	if err != nil {
		return nil, err
	}

	var version api.VersionResponse
	if err := DecodeResponse(resp, &version); err != nil {
		return nil, err
	}

	return &version, nil
}
