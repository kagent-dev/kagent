package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// Version defines the version-related operations
type Version interface {
	GetVersion(ctx context.Context) (*apiv1alpha1.GetVersionResponse, error)
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
func (c *versionClient) GetVersion(ctx context.Context) (*apiv1alpha1.GetVersionResponse, error) {
	client, err := c.client.systemServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	return client.GetVersion(callContext, &apiv1alpha1.GetVersionRequest{})
}

func (c *BaseClient) systemServiceClient() (apiv1alpha1.SystemServiceClient, error) {
	connection, err := c.grpcConnection(c.api)
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewSystemServiceClient(connection), nil
}
