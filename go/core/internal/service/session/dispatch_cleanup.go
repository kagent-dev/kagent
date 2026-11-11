package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
)

const dispatchCleanupMaxRetryDelay = 15 * time.Second

// dispatchCleanup retains the owner and completed phases between bounded
// attempts. A finish callback records a known outcome without more actor calls.
type dispatchCleanup struct {
	work *database.SessionDispatchCleanup
	// Remember a successful resume even if persisting SUSPENDING must retry.
	suspending bool
	finish     func(context.Context) error
	retryDelay time.Duration
}

// claimAbandonedDispatch claims delivery before any runtime I/O, so only its
// owner reads the actor. A new delivery can supersede this worker at any time.
func (w *ActorWorkflow) claimAbandonedDispatch(ctx context.Context, afterSessionID string) (*dispatchCleanup, error) {
	runtimeCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	work, err := w.store.ClaimSessionDispatchCleanup(runtimeCtx, afterSessionID)
	if err != nil {
		return nil, err
	}
	w.recordDispatchCleanup(ctx, work, lifecycleEventClaim, nil)
	return &dispatchCleanup{work: work, suspending: work.Suspending}, nil
}

// retryDispatchCleanup performs one bounded runtime or persistence attempt.
// A retry yields its worker and continues only while the original dispatch is
// current. An expired executor can be replaced without blocking new messages.
func (w *ActorWorkflow) retryDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup) (done bool, delay time.Duration, err error) {
	if err := ctx.Err(); err != nil && cleanup.finish == nil {
		return true, 0, err
	}
	retryable, err := w.attemptDispatchCleanup(ctx, cleanup)
	if err == nil && cleanup.finish != nil {
		return true, 0, nil
	}
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
		w.recordDispatchCleanup(ctx, cleanup.work, lifecycleEventStopped, err)
		return true, 0, nil // This owner is obsolete or blocked; durable work remains discoverable.
	}
	if err != nil && !retryable {
		return true, 0, err
	}
	if ctx.Err() != nil {
		return true, 0, ctx.Err()
	}
	if err == nil {
		cleanup.retryDelay = 0
		return false, 0, nil
	}
	w.recordDispatchCleanup(ctx, cleanup.work, lifecycleEventRetry, err)
	if cleanup.retryDelay == 0 {
		cleanup.retryDelay = 100 * time.Millisecond
	} else {
		cleanup.retryDelay = min(2*cleanup.retryDelay, dispatchCleanupMaxRetryDelay)
	}
	return false, cleanup.retryDelay, nil
}

// attemptDispatchCleanup bounds runtime work and persistence separately. Store
// failures can retry; runtime failures use the narrower RPC error policy. The
// caller handles lost ownership and decides when to schedule another attempt.
func (w *ActorWorkflow) attemptDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup) (retryable bool, err error) {
	retryable = true
	if cleanup.finish == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
		err = w.continueDispatchCleanup(attemptCtx, cleanup.work, cleanup.suspending)
		if err == nil {
			err = w.advanceDispatchCleanup(attemptCtx, cleanup)
			retryable = retryableCleanupCall(err)
		}
		cancel()
	}
	if err != nil || cleanup.finish == nil {
		return retryable, err
	}
	// Persist a known outcome even on shutdown. Retries of this callback must
	// not read or mutate the actor again.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return true, cleanup.finish(finishCtx)
}

// continueDispatchCleanup records a phase transition only after its transaction
// commits. Renewing an unchanged phase does not count as forward progress.
func (w *ActorWorkflow) continueDispatchCleanup(ctx context.Context, work *database.SessionDispatchCleanup, suspending bool) error {
	previous := work.Suspending
	if err := w.store.ContinueSessionDispatchCleanup(ctx, work, suspending); err != nil {
		return err
	}
	if suspending && !previous {
		w.recordDispatchCleanup(ctx, work, lifecycleEventSuspend, nil)
	}
	return nil
}

// advanceDispatchCleanup rechecks the dispatch after observing the actor and
// before each mutation. Persisted suspension intent survives worker replacement;
// an already-issued call may finish after a new message supersedes this cleanup.
func (w *ActorWorkflow) advanceDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup) error {
	work := cleanup.work
	revision, actor, err := w.prepareQuiescence(ctx, work.Session)
	if err != nil {
		return err
	}
	if actor.GetMetadata().GetUid() != work.ActorUID {
		return fmt.Errorf("runtime Actor changed during abandoned dispatch cleanup")
	}
	state := actor.GetStatus().GetState()
	previousSuspension := !cleanup.suspending && (state == ateapipb.ActorState_ACTOR_STATE_SUSPENDING || state == ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
	if previousSuspension {
		// An earlier suspension cannot rule out a delayed ingress resume. Park
		// durably so startup and repeated scans preserve the slower probe time.
		cleanup.finish = func(finishCtx context.Context) error {
			if err := w.store.ParkSessionDispatchCleanup(finishCtx, work); err != nil {
				return err
			}
			w.recordDispatchCleanup(finishCtx, work, lifecycleEventPark, nil)
			return nil
		}
		return nil
	}
	if state == ateapipb.ActorState_ACTOR_STATE_CRASHED {
		// A confirmed crash permits the normal recovery path to take over.
		cleanup.finish = func(finishCtx context.Context) error {
			if err := w.store.ReleaseSessionDispatchCleanup(finishCtx, work); err != nil {
				return err
			}
			w.recordDispatchCleanup(finishCtx, work, lifecycleEventRelease, nil)
			return nil
		}
		return nil
	}
	if !cleanup.suspending && state == ateapipb.ActorState_ACTOR_STATE_RESUMING {
		return w.resumeForDispatchCleanup(ctx, cleanup, revision, actor)
	}
	if state != ateapipb.ActorState_ACTOR_STATE_RUNNING && state != ateapipb.ActorState_ACTOR_STATE_SUSPENDING && state != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return fmt.Errorf("runtime Actor cannot continue abandoned dispatch cleanup in state %s", state)
	}
	return w.suspendForDispatchCleanup(ctx, cleanup, revision, actor)
}

// resumeForDispatchCleanup revalidates ownership before completing an interrupted
// resume. Remember success locally so a failed phase write cannot cause another resume.
func (w *ActorWorkflow) resumeForDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup, revision *database.RuntimeRevision, actor *ateapipb.Actor) error {
	if err := w.continueDispatchCleanup(ctx, cleanup.work, false); err != nil {
		return err
	}
	if err := w.resumeQuiescentActor(ctx, cleanup.work.Session, revision, actor); err != nil {
		return err
	}
	cleanup.suspending = true
	return nil
}

// suspendForDispatchCleanup commits suspension intent before issuing the call.
// Once the snapshot is known, only its conditional persistence may be retried.
func (w *ActorWorkflow) suspendForDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup, revision *database.RuntimeRevision, actor *ateapipb.Actor) error {
	work := cleanup.work
	if err := w.continueDispatchCleanup(ctx, work, true); err != nil {
		return err
	}
	cleanup.suspending = true
	snapshot, err := w.suspendQuiescentActor(ctx, work.Session, revision, actor)
	if err != nil {
		return err
	}
	cleanup.finish = func(finishCtx context.Context) error {
		if err := w.store.FinishSessionDispatchCleanup(finishCtx, work, snapshot); err != nil {
			return err
		}
		w.recordDispatchCleanup(finishCtx, work, lifecycleEventComplete, nil)
		return nil
	}
	return nil
}

// retryableCleanupCall includes Internal because Substrate can wrap interrupted
// restores with that code. Local identity and snapshot validation errors stop.
func retryableCleanupCall(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Internal, codes.ResourceExhausted, codes.FailedPrecondition:
		return true
	default:
		return false
	}
}
