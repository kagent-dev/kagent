package substrate

import (
	"context"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	actorDeletePollInterval = 2 * time.Second
	actorDeleteTimeout      = 5 * time.Minute
)

// deleteActorSequenced suspends the actor, waits until suspended, deletes it, and waits until gone.
func (c *Client) deleteActorSequenced(ctx context.Context, actorID string) error {
	if actorID == "" {
		return nil
	}
	deadline := time.Now().Add(actorDeleteTimeout)

	actor, err := c.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("get actor %q: %w", actorID, err)
	}

	if err := c.ensureActorSuspended(ctx, actorID, actor.GetStatus(), deadline); err != nil {
		return err
	}

	if err := c.DeleteActor(ctx, actorID); err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if status.Code(err) == codes.FailedPrecondition {
			// ate-api requires STATUS_SUSPENDED; re-check and surface current status.
			actor, getErr := c.GetActor(ctx, actorID)
			if getErr == nil {
				return fmt.Errorf("delete actor %q: not suspended (status %s)", actorID, actor.GetStatus())
			}
		}
		return fmt.Errorf("delete actor %q: %w", actorID, err)
	}

	return c.waitForActorDeleted(ctx, actorID, deadline)
}

func (c *Client) ensureActorSuspended(ctx context.Context, actorID string, st ateapipb.Actor_Status, deadline time.Time) error {
	switch st {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED:
		return nil
	case ateapipb.Actor_STATUS_SUSPENDING:
		// Retry suspend periodically; stuck checkpoint may need manual worker pod deletion.
		_ = c.SuspendActor(ctx, actorID)
		return c.waitForActorStatus(ctx, actorID, ateapipb.Actor_STATUS_SUSPENDED, deadline)
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		if err := c.SuspendActor(ctx, actorID); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("suspend actor %q: %w", actorID, err)
		}
		return c.waitForActorStatus(ctx, actorID, ateapipb.Actor_STATUS_SUSPENDED, deadline)
	default:
		// Best-effort suspend for unknown/intermediate states before delete.
		_ = c.SuspendActor(ctx, actorID)
		return c.waitForActorStatus(ctx, actorID, ateapipb.Actor_STATUS_SUSPENDED, deadline)
	}
}

func (c *Client) waitForActorStatus(ctx context.Context, actorID string, want ateapipb.Actor_Status, deadline time.Time) error {
	for time.Now().Before(deadline) {
		actor, err := c.GetActor(ctx, actorID)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				if want == ateapipb.Actor_STATUS_UNSPECIFIED {
					return nil
				}
				return fmt.Errorf("actor %q not found while waiting for %s", actorID, want)
			}
			return fmt.Errorf("get actor %q: %w", actorID, err)
		}
		if actor.GetStatus() == want {
			return nil
		}
		if want == ateapipb.Actor_STATUS_SUSPENDED && actor.GetStatus() == ateapipb.Actor_STATUS_SUSPENDING {
			if err := sleepOrDone(ctx, actorDeletePollInterval); err != nil {
				return err
			}
			continue
		}
		if err := sleepOrDone(ctx, actorDeletePollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("timeout waiting for actor %q status %s", actorID, want)
}

func (c *Client) waitForActorDeleted(ctx context.Context, actorID string, deadline time.Time) error {
	for time.Now().Before(deadline) {
		_, err := c.GetActor(ctx, actorID)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil
			}
			return fmt.Errorf("get actor %q: %w", actorID, err)
		}
		if err := sleepOrDone(ctx, actorDeletePollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("timeout waiting for actor %q deletion", actorID)
}

func sleepOrDone(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
