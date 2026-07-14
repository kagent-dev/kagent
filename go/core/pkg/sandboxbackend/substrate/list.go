package substrate

import (
	"context"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// ListActors returns all actors in the given atespace (empty atespace = all atespaces,
// including substrate's reserved golden atespace). The list API is paginated — pages are
// followed until the token drains, since a single page may miss actors.
func (c *Client) ListActors(ctx context.Context, atespace string) ([]*ateapipb.Actor, error) {
	if c == nil {
		return nil, nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var actors []*ateapipb.Actor
	pageToken := ""
	for {
		resp, err := c.ControlClient.ListActors(ctx, &ateapipb.ListActorsRequest{
			Atespace:  atespace,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		actors = append(actors, resp.GetActors()...)
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return actors, nil
		}
	}
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
	case ateapipb.Actor_STATUS_PAUSING:
		return "Pausing"
	case ateapipb.Actor_STATUS_PAUSED:
		return "Paused"
	case ateapipb.Actor_STATUS_UNSPECIFIED:
		return "Unknown"
	default:
		return status.String()
	}
}
