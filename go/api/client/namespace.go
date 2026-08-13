package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
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
	client, err := c.client.systemServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListNamespaces(callContext, &apiv1alpha1.ListNamespacesRequest{})
	if err != nil {
		return nil, err
	}

	namespaces := make([]api.NamespaceResponse, 0, len(response.GetNamespaces()))
	for _, namespace := range response.GetNamespaces() {
		namespaces = append(namespaces, api.NamespaceResponse{
			Name:   namespace.GetName(),
			Status: namespace.GetStatus(),
		})
	}
	result := api.NewResponse(namespaces, "Successfully listed namespaces", false)
	return &result, nil
}

func (c *BaseClient) systemServiceClient() (apiv1alpha1.SystemServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewSystemServiceClient(connection), nil
}
