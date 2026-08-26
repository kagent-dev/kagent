package substrate

import (
	"context"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// actorPageSize is what ate-api is asked for per page.
//
// Its maximum: "values above 1000 are coerced to 1000". Left unset the server picks a
// much smaller default, and on a deployment holding 410,110 actors that is thousands of
// round trips for one walk — which measured at ~1.6s per read of the inventory.
const actorPageSize = 1000

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
			PageSize:  actorPageSize,
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

// EachActorPage calls visit with each page of actors as it arrives, instead of
// accumulating them.
//
// ListActors above collects every page into one slice, which is fine for a small
// cluster and fatal for a large one: a deployment reporting 410,110 actors put
// several hundred megabytes of protos in the controller and OOM-killed it. A
// caller that only needs to count, filter or take one page never has to hold them
// all, and this is how it avoids doing so.
//
// visit must not retain the slice it is given — the next page reuses nothing, but
// the actors themselves are only guaranteed to live as long as the call. Returning
// an error from visit stops the walk and is returned as-is.
func (c *Client) EachActorPage(ctx context.Context, atespace string, visit func([]*ateapipb.Actor) error) error {
	if c == nil {
		return nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	pageToken := ""
	for {
		resp, err := c.ControlClient.ListActors(ctx, &ateapipb.ListActorsRequest{
			Atespace:  atespace,
			PageSize:  actorPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return err
		}
		if err := visit(resp.GetActors()); err != nil {
			return err
		}
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return nil
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
func ActorStatusLabel(status ateapipb.ActorState) string {
	switch status {
	case ateapipb.ActorState_ACTOR_STATE_RESUMING:
		return "Resuming"
	case ateapipb.ActorState_ACTOR_STATE_RUNNING:
		return "Running"
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
		return "Suspending"
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
		return "Suspended"
	case ateapipb.ActorState_ACTOR_STATE_PAUSING:
		return "Pausing"
	case ateapipb.ActorState_ACTOR_STATE_PAUSED:
		return "Paused"
	case ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED:
		return "Unknown"
	default:
		return status.String()
	}
}
