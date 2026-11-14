package client

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// HarnessClient provides supported Harness operations.
type HarnessClient struct {
	client *baseClient
}

// newHarnessClient creates a Harness client over the shared gRPC connection.
func newHarnessClient(client *baseClient) *HarnessClient {
	return &HarnessClient{client: client}
}

func (c *HarnessClient) ListHarnesses(ctx context.Context, request *apiv1.ListHarnessesRequest) (*apiv1.ListHarnessesResponse, error) {
	client, callContext, cancel, err := c.client.harnessCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListHarnesses(callContext, request)
}

func (c *baseClient) harnessCall(ctx context.Context) (apiv1.HarnessServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1.NewHarnessServiceClient(connection), callContext, cancel, nil
}
