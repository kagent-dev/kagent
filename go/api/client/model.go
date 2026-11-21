package client

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// ModelClient provides supported ModelConfig operations.
type ModelClient struct {
	client *baseClient
}

// newModelClient creates a ModelConfig client over the shared gRPC connection.
func newModelClient(client *baseClient) *ModelClient {
	return &ModelClient{client: client}
}

func (c *ModelClient) ListModelConfigs(ctx context.Context, request *apiv1.ListModelConfigsRequest) (*apiv1.ListModelConfigsResponse, error) {
	client, callContext, cancel, err := c.client.modelCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListModelConfigs(callContext, request)
}

func (c *baseClient) modelCall(ctx context.Context) (apiv1.ModelServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1.NewModelServiceClient(connection), callContext, cancel, nil
}
