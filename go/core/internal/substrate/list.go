package substrate

import (
	"context"
	"strings"

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
//
// Never a wire constant, including for a state this build has not been taught: callers
// sort on what we send and show what they sort, so `ACTOR_STATE_DELETING` would file
// itself under A while reading as "Deleting" under a heading that says sorted by status.
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
		return humanizeState(status.String())
	}
}

// humanizeState turns `ACTOR_STATE_DELETING` into `Deleting`. Protobuf names each value
// after its own enum, and that prefix only repeats the column it is shown in.
func humanizeState(name string) string {
	words := strings.ToLower(strings.TrimPrefix(name, "ACTOR_STATE_"))
	words = strings.ReplaceAll(words, "_", " ")
	if words == "" {
		return name
	}
	return strings.ToUpper(words[:1]) + words[1:]
}
