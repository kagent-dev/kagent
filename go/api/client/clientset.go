package client

import (
	"context"
	"errors"
	"fmt"

	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

// APIClientSet contains control-plane API clients.
type APIClientSet struct {
	client        *baseClient
	Version       Version
	AgentInstance *AgentInstanceClient
}

// NewAPI creates a control-plane API client set.
func NewAPI(apiURL string, options ...ClientOption) (*APIClientSet, error) {
	client, err := newBaseClient(apiURL, options...)
	if err != nil {
		return nil, err
	}
	return &APIClientSet{
		client:        client,
		Version:       newVersionClient(client),
		AgentInstance: newAgentInstanceClient(client),
	}, nil
}

// Close releases transport resources owned by the API client set.
func (c *APIClientSet) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// GatewayClientSet contains agent-traffic clients.
type GatewayClientSet struct {
	client *baseClient
	A2A    *A2AClient
}

// NewGateway creates an agent-traffic client set.
func NewGateway(gatewayURL string, options ...ClientOption) (*GatewayClientSet, error) {
	client, err := newBaseClient(gatewayURL, options...)
	if err != nil {
		return nil, err
	}
	return &GatewayClientSet{client: client, A2A: newA2AClient(client)}, nil
}

// Close releases transport resources owned by the gateway client set.
func (c *GatewayClientSet) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// CheckHealth verifies that an endpoint's gRPC server is ready.
func CheckHealth(ctx context.Context, rawURL string, options ...ClientOption) (err error) {
	client, err := newBaseClient(rawURL, options...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, client.Close()) }()
	connection, err := client.grpcConnection()
	if err != nil {
		return err
	}
	callContext, cancel := client.grpcCallContext(ctx)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(callContext, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("gRPC server is not serving: %s", response.GetStatus())
	}
	return nil
}
