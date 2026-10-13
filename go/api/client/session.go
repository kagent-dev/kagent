package client

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// SessionClient provides supported Session operations.
type SessionClient struct {
	client *baseClient
}

func newSessionClient(client *baseClient) *SessionClient {
	return &SessionClient{client: client}
}

func (c *SessionClient) CreateSession(ctx context.Context, request *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
	client, callContext, cancel, err := c.client.sessionCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.CreateSession(callContext, request)
}

func (c *SessionClient) GetSession(ctx context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
	client, callContext, cancel, err := c.client.sessionCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.GetSession(callContext, request)
}

func (c *SessionClient) ListSessions(ctx context.Context, request *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error) {
	client, callContext, cancel, err := c.client.sessionCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.ListSessions(callContext, request)
}

func (c *SessionClient) DeleteSession(ctx context.Context, request *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error) {
	client, callContext, cancel, err := c.client.sessionCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.DeleteSession(callContext, request)
}

func (c *baseClient) sessionCall(ctx context.Context) (apiv1alpha1.SessionServiceClient, context.Context, context.CancelFunc, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	callContext, cancel := c.grpcCallContext(ctx)
	return apiv1alpha1.NewSessionServiceClient(connection), callContext, cancel, nil
}
