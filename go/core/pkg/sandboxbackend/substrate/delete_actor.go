package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// deleteActor performs at most one mutating ate-api step per call.
// Returns true when the actor no longer exists. Callers should requeue until true.
func deleteActor(ctx context.Context, c *Client, atespace, actorID string) (bool, error) {
	if actorID == "" {
		return true, nil
	}
	if c == nil {
		return false, fmt.Errorf("substrate ate-api client is required")
	}

	actor, err := c.GetActor(ctx, atespace, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, fmt.Errorf("get actor %q: %w", actorID, err)
	}

	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED:
		if err := c.DeleteActor(ctx, atespace, actorID); err != nil {
			if status.Code(err) == codes.NotFound {
				return true, nil
			}
			if status.Code(err) == codes.FailedPrecondition {
				return false, fmt.Errorf("delete actor %q: not suspended (status %s)", actorID, actor.GetStatus().GetState())
			}
			return false, fmt.Errorf("delete actor %q: %w", actorID, err)
		}
		return false, nil
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
		_, _ = c.SuspendActor(ctx, atespace, actorID)
		return false, nil
	case ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_RESUMING:
		if _, err := c.SuspendActor(ctx, atespace, actorID); err != nil && status.Code(err) != codes.NotFound {
			return false, fmt.Errorf("suspend actor %q: %w", actorID, err)
		}
		return false, nil
	case ateapipb.ActorState_ACTOR_STATE_PAUSED:
		if _, err := c.ResumeActor(ctx, atespace, actorID); err != nil && status.Code(err) != codes.NotFound {
			return false, fmt.Errorf("resume paused actor %q before delete: %w", actorID, err)
		}
		return false, nil
	case ateapipb.ActorState_ACTOR_STATE_PAUSING:
		return false, nil
	default:
		_, _ = c.SuspendActor(ctx, atespace, actorID)
		return false, nil
	}
}
