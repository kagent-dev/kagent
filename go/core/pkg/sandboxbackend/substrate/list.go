package substrate

import (
	"context"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// ListActors returns all actors reflected in ate-api.
func (c *Client) ListActors(ctx context.Context) ([]*ateapipb.Actor, error) {
	if c == nil {
		return nil, nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ListActors(ctx, &ateapipb.ListActorsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetActors(), nil
}

// ListWorkers returns all workers reflected in ate-api.
func (c *Client) ListWorkers(ctx context.Context) ([]*ateapipb.Worker, error) {
	if c == nil {
		return nil, nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ListWorkers(ctx, &ateapipb.ListWorkersRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetWorkers(), nil
}

// ActorStatusLabel returns a stable human-readable actor status.
func ActorStatusLabel(status ateapipb.Actor_Status) string {
	switch status {
	case ateapipb.Actor_STATUS_RESUMING:
		return "Resuming"
	case ateapipb.Actor_STATUS_RUNNING:
		return "Running"
	case ateapipb.Actor_STATUS_SUSPENDING:
		return "Suspending"
	case ateapipb.Actor_STATUS_SUSPENDED:
		return "Suspended"
	case ateapipb.Actor_STATUS_UNSPECIFIED:
		return "Unknown"
	default:
		return status.String()
	}
}
