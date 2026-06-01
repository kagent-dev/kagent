package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// deleteActor performs at most one mutating ate-api step per call.
// Returns true when the actor no longer exists. Callers should requeue until true.
func deleteActor(ctx context.Context, c *Client, actorID string) (bool, error) {
	if actorID == "" {
		return true, nil
	}
	if c == nil {
		return false, fmt.Errorf("substrate ate-api client is required")
	}

	actor, err := c.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, fmt.Errorf("get actor %q: %w", actorID, err)
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED:
		if err := c.DeleteActor(ctx, actorID); err != nil {
			if status.Code(err) == codes.NotFound {
				return true, nil
			}
			if status.Code(err) == codes.FailedPrecondition {
				return false, fmt.Errorf("delete actor %q: not suspended (status %s)", actorID, actor.GetStatus())
			}
			return false, fmt.Errorf("delete actor %q: %w", actorID, err)
		}
		return false, nil
	case ateapipb.Actor_STATUS_SUSPENDING:
		_ = c.SuspendActor(ctx, actorID)
		return false, nil
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		if err := c.SuspendActor(ctx, actorID); err != nil && status.Code(err) != codes.NotFound {
			return false, fmt.Errorf("suspend actor %q: %w", actorID, err)
		}
		return false, nil
	default:
		_ = c.SuspendActor(ctx, actorID)
		return false, nil
	}
}
