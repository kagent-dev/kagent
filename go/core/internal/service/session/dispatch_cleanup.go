package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/util/workqueue"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
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
	claimID   uuid.UUID
	uncertain bool // An unanswered SuspendActor may still finish. Only observe it on retry.
	notBefore time.Time
	deadline  time.Time
	work      *database.SessionDispatchCleanup
	snapshot  *database.SessionTaskSnapshot
}

// QueueDispatchCleanup schedules best-effort cleanup after an unused dispatch is
// revoked and its router connection is closed. A configurable grace period lets
// users retry first; zero disables cleanup. New sends supersede cleanup until it claims suspension.
func (w *ActorWorkflow) QueueDispatchCleanup(ctx context.Context, sessionID string, cleanupID uuid.UUID) {
	if w.dispatchCleanupDelay == 0 {
		return
	}
	// Bound the whole queue, including retries waiting in the delaying queue.
	if w.dispatchCleanupCount.Add(1) > dispatchCleanupQueueSize {
		w.dispatchCleanupCount.Add(-1)
		w.dispatchDrops.Add(ctx, 1, metric.WithAttributes(conv.KagentDispatchCleanupDropReasonKey.String(conv.KagentDispatchCleanupDropReasonQueueFull)))
		logging.FromContext(ctx).WarnContext(ctx, "dispatch cleanup queue full", "session_id", sessionID)
		return
	}
	notBefore := time.Now().Add(w.dispatchCleanupDelay)
	request := &dispatchCleanup{sessionID: sessionID, cleanupID: cleanupID, claimID: uuid.New(), notBefore: notBefore, deadline: notBefore.Add(dispatchCleanupBudget)}
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
					if err != nil && ctx.Err() == nil {
						w.dispatchDrops.Add(ctx, 1, metric.WithAttributes(conv.KagentDispatchCleanupDropReasonKey.String(conv.KagentDispatchCleanupDropReasonRetryBudgetExhausted)))
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
			queue.AddAfter(request, time.Until(request.notBefore))
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
		work, err := w.store.GetSessionDispatchCleanup(attempt, request.sessionID, request.cleanupID, request.claimID)
		if err == nil {
			request.snapshot, err = w.cleanAbandonedActor(attempt, request, work)
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
	if errors.Is(err, database.ErrNotFound) {
		return nil
	}
	return err
}

// cleanAbandonedActor leaves crashed, missing, or replaced actors to explicit
// recovery. A RESUMING actor must finish resuming before Substrate can suspend it.
func (w *ActorWorkflow) cleanAbandonedActor(ctx context.Context, request *dispatchCleanup, work *database.SessionDispatchCleanup) (*database.SessionTaskSnapshot, error) {
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
	state := actor.GetStatus().GetState()
	switch state {
	case ateapipb.ActorState_ACTOR_STATE_CRASHED:
		return nil, nil
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
		if work.TaskID == "" {
			return nil, nil
		}
		return actorVolumeSnapshot(actor)
	}
	// A suspend whose reply was lost may still finish. Observe its outcome
	// without issuing another mutation that could arrive after admission reopens.
	if request.uncertain {
		return nil, fmt.Errorf("awaiting issued suspension: actor is %s", state)
	}
	switch state {
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
	claimed, err := w.store.ClaimSessionDispatchSuspension(ctx, work.Session.Id, request.cleanupID, request.claimID)
	if err != nil {
		return nil, err
	}
	*work = *claimed
	snapshot, err := w.suspendVerifiedActor(ctx, revision, actor)
	// Substrate returns these application errors when it rejects or aborts the
	// operation. A retry can continue under the claim. A transport error leaves
	// execution uncertain, so subsequent attempts only read the actor.
	request.uncertain = err != nil && status.Code(err) != codes.Aborted && status.Code(err) != codes.FailedPrecondition
	return snapshot, err
}
