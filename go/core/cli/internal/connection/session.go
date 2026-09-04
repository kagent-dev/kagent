package connection

import (
	"context"
	"errors"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/client"
)

// Session is a connected kagent client for one command invocation, together
// with the namespace the command is scoped to.
type Session struct {
	API       *client.APIClientSet
	Gateway   *client.GatewayClientSet
	Namespace string

	portForward *PortForward
}

// Open reaches the server, starting a port-forward when the default local
// endpoint is unreachable. The caller must Close the returned session.
func Open(ctx context.Context, options Options) (*Session, error) {
	portForward, err := Connect(ctx, &options, options.APIURL)
	if err != nil {
		return nil, fmt.Errorf("connect to kagent: %w", err)
	}
	api, err := options.APIClient()
	if err != nil {
		portForward.Stop()
		return nil, fmt.Errorf("create kagent API client: %w", err)
	}
	gateway, err := options.GatewayClient()
	if err != nil {
		_ = api.Close()
		portForward.Stop()
		return nil, fmt.Errorf("create kagent gateway client: %w", err)
	}
	return &Session{
		API:         api,
		Gateway:     gateway,
		Namespace:   options.Namespace,
		portForward: portForward,
	}, nil
}

// OpenAPI connects only to the control-plane API endpoint.
func OpenAPI(ctx context.Context, options Options) (*Session, error) {
	portForward, err := Connect(ctx, &options, options.APIURL)
	if err != nil {
		return nil, fmt.Errorf("connect to kagent API: %w", err)
	}
	api, err := options.APIClient()
	if err != nil {
		portForward.Stop()
		return nil, fmt.Errorf("create kagent API client: %w", err)
	}
	return &Session{API: api, Namespace: options.Namespace, portForward: portForward}, nil
}

// OpenGateway connects only to the agent-traffic endpoint.
func OpenGateway(ctx context.Context, options Options) (*Session, error) {
	portForward, err := Connect(ctx, &options, options.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("connect to kagent gateway: %w", err)
	}
	gateway, err := options.GatewayClient()
	if err != nil {
		portForward.Stop()
		return nil, fmt.Errorf("create kagent gateway client: %w", err)
	}
	return &Session{Gateway: gateway, Namespace: options.Namespace, portForward: portForward}, nil
}

// Close releases the client before tearing down the port-forward it rode on.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	err := errors.Join(s.API.Close(), s.Gateway.Close())
	s.portForward.Stop()
	return err
}
