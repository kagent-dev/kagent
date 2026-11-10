package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

var _ manager.Runnable = (*ActorWorkflow)(nil)
var _ manager.LeaderElectionRunnable = (*ActorWorkflow)(nil)

// Every API replica can process idle work; PostgreSQL grants each claim once.
func (*ActorWorkflow) NeedLeaderElection() bool { return false }

// wakeQuiescence is a bounded, nonblocking hint. A buffered signal survives a
// settlement racing with a worker's empty scan or a burst while all workers are
// busy. PostgreSQL, rather than the channel, holds the work to drain.
func (w *ActorWorkflow) wakeQuiescence() {
	select {
	case w.quiescenceWake <- struct{}{}:
	default:
	}
}

// Start suspends idle sessions to VOLUMES independently of task publication.
// Startup and periodic recovery scans discover work missed by local signals.
// Workers drain claims with bounded concurrency; task reads never wait for them.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			timer := time.NewTimer(w.recoveryInterval)
			defer timer.Stop()
			var afterSessionID string
			for ctx.Err() == nil {
				nextSessionID, err := w.quiesceNextSession(ctx, afterSessionID)
				afterSessionID = nextSessionID
				if err == nil {
					continue
				}
				delay := min(time.Second, w.recoveryInterval)
				if errors.Is(err, database.ErrNotFound) {
					pending, pendingErr := w.store.HasPendingSessionQuiescence(ctx)
					if pendingErr != nil {
						err = pendingErr
					} else if !pending {
						delay = w.recoveryInterval
					}
				}
				if !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
					logging.FromContext(ctx).ErrorContext(ctx, "claim runtime boundary", "error", err)
				}
				timer.Reset(delay)
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				case <-w.quiescenceWake:
				}
				afterSessionID = ""
			}
		})
	}
	workers.Wait()
	return nil
}

// quiesceNextSession processes one abandoned delivery or task awaiting suspension.
// It returns the last Session ID scanned; ErrNotFound means neither has work.
func (w *ActorWorkflow) quiesceNextSession(ctx context.Context, afterSessionID string) (string, error) {
	cleanup, err := w.store.GetPendingSessionDispatchCleanup(ctx, afterSessionID)
	if err == nil {
		// Visit each candidate once per scan. An unfinished restore must not
		// busy-loop or hide later Sessions while it waits for the next scan.
		return cleanup.Session.Id, w.quiesceAbandonedDispatch(ctx, cleanup)
	}
	if !errors.Is(err, database.ErrNotFound) {
		return afterSessionID, err
	}
	work, err := w.store.ClaimSessionQuiescence(ctx)
	if err != nil {
		return afterSessionID, err
	}
	// Hand off a hint before runtime I/O so a burst can enlist all workers.
	w.wakeQuiescence()
	w.quiesceIdleSession(ctx, work)
	return afterSessionID, nil
}

// quiesceAbandonedDispatch suspends a running actor after canceled or expired
// delivery. New sends remain allowed until the worker claims suspension, which
// checks that no turn or newer delivery has started.
func (w *ActorWorkflow) quiesceAbandonedDispatch(ctx context.Context, candidate *database.SessionDispatchCleanup) error {
	runtimeCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	revision, actor, err := w.prepareQuiescence(runtimeCtx, candidate.Session)
	if err == nil && actor.GetMetadata().GetUid() != candidate.ActorUID {
		err = fmt.Errorf("runtime Actor changed before abandoned dispatch cleanup")
	}
	if err != nil {
		if ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "prepare abandoned dispatch cleanup", "session_id", candidate.Session.Id, "error", err)
		}
		return nil
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		// SUSPENDED is not proof that canceled ingress cannot resume later.
		// RESUMING cannot be suspended yet. Neither warrants an issued claim.
		return nil
	}
	work, err := w.store.ClaimSessionDispatchCleanup(runtimeCtx, candidate)
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
		return nil // A new send or another lifecycle worker won admission.
	}
	if err != nil {
		return err
	}
	w.wakeQuiescence()
	var snapshot *database.SessionTaskSnapshot
	for {
		snapshot, err = w.suspendQuiescentActor(runtimeCtx, work.Session, revision, actor)
		if status.Code(err) != codes.Aborted {
			break
		}
		// Substrate permits retrying an aborted workflow. Retain the same
		// claim regardless of earlier effects so no new send races the retry.
		select {
		case <-runtimeCtx.Done():
			err = runtimeCtx.Err()
		case <-time.After(100 * time.Millisecond):
			continue
		}
		break
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "abandoned dispatch suspension outcome unknown", "session_id", work.Session.Id, "dispatch_id", work.DispatchID, "error", err)
		return nil // A possibly issued call cannot safely release admission.
	}
	w.recordQuiescence(ctx, work.Session.Id, func(finishCtx context.Context) error {
		return w.store.FinishSessionDispatchCleanup(finishCtx, work, snapshot)
	})
	return nil
}

// quiesceIdleSession suspends an actor for a claimed task and records its snapshot.
func (w *ActorWorkflow) quiesceIdleSession(ctx context.Context, work *database.SessionQuiescence) {
	runtimeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	snapshot, err := w.Quiesce(runtimeCtx, work.Session)
	cancel()
	if err != nil {
		// No timeout-based takeover: the Substrate request may still complete.
		// Keep admission closed until its outcome can be safely reconciled.
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary outcome unknown", "session_id", work.Session.Id, "version", work.Version, "error", err)
		return
	}
	w.recordQuiescence(ctx, work.Session.Id, func(finishCtx context.Context) error {
		return w.store.FinishSessionQuiescence(finishCtx, work, snapshot)
	})
}

// recordQuiescence retries saving a completed suspension without suspending again.
// Each write is bounded and may finish after shutdown; further retries stop.
func (w *ActorWorkflow) recordQuiescence(ctx context.Context, sessionID string, finish func(context.Context) error) {
	for delay := 100 * time.Millisecond; ; delay = min(2*delay, 5*time.Second) {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := finish(finishCtx)
		finishCancel()
		if err == nil {
			return
		}
		logging.FromContext(ctx).ErrorContext(ctx, "record idle runtime outcome", "session_id", sessionID, "error", err)
		if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
