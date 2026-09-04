package substrate

import (
	"context"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// ListActorsPage returns one page of actors in the given atespace (empty atespace =
// all atespaces, including substrate's reserved golden atespace), with the token for
// the next page or "" on the last one.
//
// The page may be empty and still not be the last: ate-api says so explicitly, and a
// caller that stops on an empty page stops early.
func (c *Client) ListActorsPage(ctx context.Context, atespace string, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
	if c == nil {
		return nil, "", nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ListActors(ctx, &ateapipb.ListActorsRequest{
		Atespace:  atespace,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	return resp.GetActors(), resp.GetNextPageToken(), nil
}

// ListActors returns every actor in the given atespace, following pagination until
// the token drains.
//
// The whole inventory in memory at once: a cluster of hundreds of thousands of actors
// makes this expensive, and packing the result into a single gRPC response makes it
// impossible. Callers that answer a request with these rows want ListActorsPage;
// this one is for callers that reduce them to something small, such as counts.
func (c *Client) ListActors(ctx context.Context, atespace string) ([]*ateapipb.Actor, error) {
	if c == nil {
		return nil, nil
	}
	var actors []*ateapipb.Actor
	pageToken := ""
	for {
		page, next, err := c.ListActorsPage(ctx, atespace, 0, pageToken)
		if err != nil {
			return nil, err
		}
		actors = append(actors, page...)
		if next == "" {
			return actors, nil
		}
		pageToken = next
	}
}

// ListWorkersPage returns one page of workers, with the token for the next page or ""
// on the last one.
func (c *Client) ListWorkersPage(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error) {
	if c == nil {
		return nil, "", nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ListWorkers(ctx, &ateapipb.ListWorkersRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	return resp.GetWorkers(), resp.GetNextPageToken(), nil
}

// ListWorkers returns every worker reflected in ate-api, following pagination.
//
// ListWorkers is paginated exactly as ListActors is; this used to read one page and
// drop the token, so any fleet past ate-api's page ceiling was silently truncated and
// reported as complete.
func (c *Client) ListWorkers(ctx context.Context) ([]*ateapipb.Worker, error) {
	if c == nil {
		return nil, nil
	}
	var workers []*ateapipb.Worker
	pageToken := ""
	for {
		page, next, err := c.ListWorkersPage(ctx, 0, pageToken)
		if err != nil {
			return nil, err
		}
		workers = append(workers, page...)
		if next == "" {
			return workers, nil
		}
		pageToken = next
	}
}

// ListActorTemplates returns all templates in an atespace, following pagination.
func (c *Client) ListActorTemplates(ctx context.Context, atespace string) ([]*ateapipb.ActorTemplate, error) {
	if c == nil {
		return nil, nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var templates []*ateapipb.ActorTemplate
	pageToken := ""
	for {
		resp, err := c.ControlClient.ListActorTemplates(ctx, &ateapipb.ListActorTemplatesRequest{Atespace: atespace, PageToken: pageToken})
		if err != nil {
			return nil, err
		}
		templates = append(templates, resp.GetActorTemplates()...)
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return templates, nil
		}
	}
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
