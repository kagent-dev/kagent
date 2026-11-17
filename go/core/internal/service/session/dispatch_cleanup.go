package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/util/workqueue"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	dispatchCleanupBudget         = 2 * time.Minute
	dispatchCleanupAttemptTimeout = 30 * time.Second
	dispatchCleanupMaxBackoff     = 15 * time.Second
	dispatchCleanupQueueSize      = 64
	dispatchCleanupWorkers        = 2
)

// dispatchCleanup is deliberately process-local. Restart, queue saturation, or
// exhausting the budget may leave an idle actor running; no recovery scan or
// durable job is created. A known snapshot is retained across persistence retries.
type dispatchCleanup struct {
	sessionID string
	cleanupID uuid.UUID
	deadline  time.Time
	work      *database.SessionDispatchCleanup
	snapshot  *database.SessionTaskSnapshot
}

// QueueDispatchCleanup schedules best-effort cleanup after an unused dispatch is
// revoked and its router connection is closed. A configurable grace period lets
// users retry first; zero disables cleanup. New sends supersede cleanup without
// waiting for it.
func (w *ActorWorkflow) QueueDispatchCleanup(ctx context.Context, sessionID string, cleanupID uuid.UUID) {
	if w.dispatchCleanupDelay == 0 {
		return
	}
	// Bound the whole queue, including retries waiting in the delaying queue.
	if w.dispatchCleanupCount.Add(1) > dispatchCleanupQueueSize {
		w.dispatchCleanupCount.Add(-1)
		logging.FromContext(ctx).WarnContext(ctx, "dispatch cleanup queue full", "session_id", sessionID)
		return
	}
	request := &dispatchCleanup{sessionID: sessionID, cleanupID: cleanupID, deadline: time.Now().Add(w.dispatchCleanupDelay).Add(dispatchCleanupBudget)}
	w.dispatchCleanups <- request // A counted request always has room in this buffer.
}

// runDispatchCleanups owns the retry queue and workers for this replica. Delayed
// retries occupy no worker, and shutdown drops this best-effort queue.
func (w *ActorWorkflow) runDispatchCleanups(ctx context.Context) {
	if w.dispatchCleanupDelay == 0 {
		return
	}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedItemExponentialFailureRateLimiter[*dispatchCleanup](100*time.Millisecond, dispatchCleanupMaxBackoff))
	var workers sync.WaitGroup
	for range dispatchCleanupWorkers {
		workers.Go(func() {
			for {
				request, shutdown := queue.Get()
				if shutdown {
					return
				}
				err := w.attemptDispatchCleanup(ctx, request)
				if err != nil && ctx.Err() == nil && time.Now().Before(request.deadline) {
					queue.AddRateLimited(request)
				} else {
					queue.Forget(request)
					w.dispatchCleanupCount.Add(-1)
					if err != nil {
						logging.FromContext(ctx).WarnContext(ctx, "dispatch cleanup stopped", "session_id", request.sessionID, "error", err)
					}
				}
				queue.Done(request)
			}
		})
	}
	for {
		select {
		case request := <-w.dispatchCleanups:
			queue.AddAfter(request, time.Until(request.deadline.Add(-dispatchCleanupBudget)))
		case <-ctx.Done():
			queue.ShutDown()
			workers.Wait()
			return
		}
	}
}

// attemptDispatchCleanup stops when newer work supersedes the canceled dispatch.
// Once suspension succeeds, retries only persist its known snapshot.
func (w *ActorWorkflow) attemptDispatchCleanup(ctx context.Context, request *dispatchCleanup) error {
	ctx, cancel := context.WithDeadline(ctx, request.deadline)
	defer cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if request.work == nil {
		attempt, cancelAttempt := context.WithTimeout(ctx, dispatchCleanupAttemptTimeout)
		defer cancelAttempt()
		work, err := w.store.GetSessionDispatchCleanup(attempt, request.sessionID, request.cleanupID)
		if err == nil {
			request.snapshot, err = w.cleanAbandonedActor(attempt, work)
		}
		if err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrFailedPrecondition) {
				return nil
			}
			return err
		}
		request.work = work
	}
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelFinish()
	err := w.store.FinishSessionDispatchCleanup(finish, request.work, request.snapshot)
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
		return nil
	}
	return err
}

// cleanAbandonedActor leaves crashed, missing, or replaced actors to explicit
// recovery. A RESUMING actor must finish resuming before Substrate can suspend it.
func (w *ActorWorkflow) cleanAbandonedActor(ctx context.Context, work *database.SessionDispatchCleanup) (*database.SessionTaskSnapshot, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, work.Session.GetPreparedRevision())
	if err != nil {
		return nil, err
	}
	actor, err := w.actors.GetActor(ctx, revision.ActorTemplateAtespace, substrate.ActorName(work.Session.Id))
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validActorIdentity(actor, revision, substrate.ActorName(work.Session.Id)) {
		return nil, nil
	}
	if err := w.verifyActor(ctx, work.Session, revision, actor); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_CRASHED:
		return nil, nil
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
		if work.TaskID == "" {
			return nil, nil
		}
		return actorVolumeSnapshot(actor)
	case ateapipb.ActorState_ACTOR_STATE_RESUMING:
		resumed, err := w.actors.ResumeActor(ctx, revision.ActorTemplateAtespace, substrate.ActorName(work.Session.Id))
		if err != nil {
			return nil, fmt.Errorf("resume abandoned actor: %w", err)
		}
		if !validActorIdentity(resumed, revision, substrate.ActorName(work.Session.Id)) || resumed.GetMetadata().GetUid() != actor.GetMetadata().GetUid() || resumed.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			return nil, fmt.Errorf("resume abandoned actor returned unexpected identity or state")
		}
		actor = resumed
	case ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
		// Both states can complete suspension directly.
	default:
		return nil, fmt.Errorf("actor is busy in state %s", actor.GetStatus().GetState())
	}
	// Resume can take time. A message admitted meanwhile supersedes this work.
	// This check cannot retract a Suspend already issued: Substrate may complete
	// it after our timeout, and an overlapping message may need a client retry.
	if _, err := w.store.GetSessionDispatchCleanup(ctx, work.Session.Id, work.CleanupID); err != nil {
		return nil, err
	}
	return w.suspendVerifiedActor(ctx, revision, actor)
}
