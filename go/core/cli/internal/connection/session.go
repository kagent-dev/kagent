package connection

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/client"
)

// Session is a connected kagent client for one command invocation, together
// with the namespace the command is scoped to.
type Session struct {
	Client    *client.ClientSet
	Namespace string

	portForward *PortForward
}

// Open reaches the server, starting a port-forward when the default local
// endpoint is unreachable. The caller must Close the returned session.
func Open(ctx context.Context, options Options) (*Session, error) {
	portForward, err := Connect(ctx, &options)
	if err != nil {
		return nil, fmt.Errorf("connect to kagent: %w", err)
	}
	return &Session{
		Client:      options.Client(),
		Namespace:   options.Namespace,
		portForward: portForward,
	}, nil
}

// Close releases the client before tearing down the port-forward it rode on.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	err := s.Client.Close()
	s.portForward.Stop()
	return err
}
