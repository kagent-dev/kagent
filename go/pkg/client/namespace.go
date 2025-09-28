package client

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Namespace defines the namespace operations
type Namespace interface {
	ListNamespaces(ctx context.Context) (*api.StandardResponse[[]api.NamespaceResponse], error)
}

// namespaceClient handles namespace-related requests
type namespaceClient struct {
	client *BaseClient
}

// NewNamespaceClient creates a new namespace client
func NewNamespaceClient(client *BaseClient) Namespace {
	return &namespaceClient{client: client}
}

// ListNamespaces lists all namespaces
func (c *namespaceClient) ListNamespaces(ctx context.Context) (*api.StandardResponse[[]api.NamespaceResponse], error) {
	resp, err := c.client.Get(ctx, "/api/namespaces", "")
	if err != nil {
		return nil, err
	}

	var namespaces api.StandardResponse[[]api.NamespaceResponse]
	if err := DecodeResponse(resp, &namespaces); err != nil {
		return nil, err
	}

	return &namespaces, nil
}
