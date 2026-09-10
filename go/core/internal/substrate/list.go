package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// How many pages a drain will read before giving up: at ate-api's ceiling of 1,000
// rows a page, ten million rows.
const maxDrainPages = 10_000

// AdvancePageToken moves a paging loop on, refusing a token that repeats itself: a
// server answering with the token it was given is not advancing, and a loop that
// follows it re-reads the same page until whatever cap is above it. Exported because
// every loop over ate-api's pagination wants the check.
func AdvancePageToken(previous, next string) (string, error) {
	if next != "" && next == previous {
		return "", fmt.Errorf("ate-api repeated page token %q instead of advancing", next)
	}
	return next, nil
}

// drainPages reads every page ate-api holds into one slice. Callers answering a
// request with these rows want the paged read instead.
func drainPages[Row any](
	ctx context.Context,
	read func(ctx context.Context, pageToken string) ([]Row, string, error),
) ([]Row, error) {
	var rows []Row
	pageToken := ""
	for range maxDrainPages {
		page, next, err := read(ctx, pageToken)
		if err != nil {
			return nil, err
		}
		rows = append(rows, page...)
		if next == "" {
			return rows, nil
		}
		if pageToken, err = AdvancePageToken(pageToken, next); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("ate-api offered more than %d pages; the list was not read to the end", maxDrainPages)
}

// ListActorsPage returns one page of actors in the given atespace (empty = all of
// them, including substrate's reserved golden atespace), with the token for the next
// page or "" on the last. A page may be empty and still not be the last, so a caller
// that stops on an empty page stops early.
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

// ListActors returns every actor in the given atespace, following pagination.
//
// The whole inventory in memory at once, and never small enough to answer a request
// with: callers that do want ListActorsPage. This is for callers that reduce the rows
// to something small, such as counts.
func (c *Client) ListActors(ctx context.Context, atespace string) ([]*ateapipb.Actor, error) {
	if c == nil {
		return nil, nil
	}
	return drainPages(ctx, func(ctx context.Context, pageToken string) ([]*ateapipb.Actor, string, error) {
		return c.ListActorsPage(ctx, atespace, 0, pageToken)
	})
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

// ListWorkers returns every worker reflected in ate-api, following pagination. It is
// paged exactly as ListActors is; reading one page and dropping the token silently
// truncates any fleet past ate-api's page ceiling.
func (c *Client) ListWorkers(ctx context.Context) ([]*ateapipb.Worker, error) {
	if c == nil {
		return nil, nil
	}
	return drainPages(ctx, func(ctx context.Context, pageToken string) ([]*ateapipb.Worker, string, error) {
		return c.ListWorkersPage(ctx, 0, pageToken)
	})
}

// ListActorTemplatesPage returns one page of templates in the given atespace, with the
// token for the next page or "" on the last one.
func (c *Client) ListActorTemplatesPage(ctx context.Context, atespace string, pageToken string) ([]*ateapipb.ActorTemplate, string, error) {
	if c == nil {
		return nil, "", nil
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ListActorTemplates(ctx, &ateapipb.ListActorTemplatesRequest{
		Atespace:  atespace,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	return resp.GetActorTemplates(), resp.GetNextPageToken(), nil
}

// ListActorTemplates returns every template in an atespace, following pagination.
// Draining is right here where it is not for actors: templates are configuration, so
// operators set their count rather than the cluster.
func (c *Client) ListActorTemplates(ctx context.Context, atespace string) ([]*ateapipb.ActorTemplate, error) {
	if c == nil {
		return nil, nil
	}
	return drainPages(ctx, func(ctx context.Context, pageToken string) ([]*ateapipb.ActorTemplate, string, error) {
		return c.ListActorTemplatesPage(ctx, atespace, pageToken)
	})
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
