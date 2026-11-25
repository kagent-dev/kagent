package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

var _ manager.Runnable = (*ActorWorkflow)(nil)
var _ manager.LeaderElectionRunnable = (*ActorWorkflow)(nil)

// Every API replica can process idle work; PostgreSQL grants each claim once.
func (*ActorWorkflow) NeedLeaderElection() bool { return false }

// Start suspends idle sessions to VOLUMES independently of task publication.
// Startup and periodic recovery scans discover work missed by local signals.
// Workers drain claims with bounded concurrency; task reads never wait for them.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	counter, err := newWorkClaimCounter(ctx, w.meterProvider,
		conv.KagentSessionQuiescenceClaimed, conv.KagentSessionQuiescenceClaimedUnit, conv.KagentSessionQuiescenceClaimedDescription)
	if err != nil {
		return err
	}
	// Keep worker handoffs distinct from settlement hints for attribution.
	handoff := make(chan struct{}, 1)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			timer := time.NewTimer(w.recoveryInterval)
			defer timer.Stop()
			trigger := workStartup
			// A startup pass reconciles before its first ordinary claim, so a
			// replica restarting into stranded claims settles them.
			recovering := true
			for ctx.Err() == nil {
				// A pass that waited out the recovery interval reconciles a
				// stranded claim first. Ordinary work is otherwise always
				// preferred, and a busy queue would starve recovery, which is
				// what keeps those sessions closed.
				if recovering {
					recovering = false
					if w.reconcileStaleQuiescence(ctx) {
						continue
					}
				}
				work, err := w.store.ClaimSessionQuiescence(ctx)
				if err == nil {
					// Hand off a hint before runtime I/O so a single settlement
					// burst can enlist all workers, even if this actor is slow.
					select {
					case handoff <- struct{}{}:
					default:
					}
					recordWorkClaim(ctx, counter, trigger)
					trigger.logRecoveryClaim(ctx, operationSessionQuiescence, w.recoveryInterval, slog.String("session_id", work.Session.GetId()))
					trigger = workBacklog
					w.quiesceIdleSession(ctx, work)
					continue
				}
				delay := min(time.Second, w.recoveryInterval)
				nextTrigger := workRetry
				if errors.Is(err, database.ErrNotFound) {
					pending, pendingErr := w.store.HasPendingSessionQuiescence(ctx)
					if pendingErr != nil {
						err = pendingErr
					} else if !pending {
						delay = w.recoveryInterval
						nextTrigger = workRecovery
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
					trigger = nextTrigger
					recovering = true
				case <-w.quiescenceWake:
					trigger = workSignal
				case <-handoff:
					trigger = workBacklog
				}
			}
		})
	}
	workers.Wait()
	return nil
}

// defaultStaleQuiescenceClaim is how long a claim may go unrecorded before
// another worker reads the Actor's state instead of waiting for it. It exceeds
// the runtime deadline used below, so a suspension is never taken over while
// the worker that issued it is still inside the call.
const defaultStaleQuiescenceClaim = 5 * time.Minute

// reconcileStaleQuiescence settles one claim left behind by a suspension whose
// outcome was never recorded. Without it that claim is held forever: the
// session's answers, cancellation and deletion are all refused, and nothing
// ever looks at what became of the Actor. It reports whether it found one.
func (w *ActorWorkflow) reconcileStaleQuiescence(ctx context.Context) bool {
	work, err := w.store.ClaimStaleSessionQuiescence(ctx, w.staleClaim)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "claim stranded runtime boundary", "error", err)
		}
		return false
	}
	runtimeCtx, cancel := context.WithTimeout(ctx, time.Minute)
	snapshot, outcome, err := w.reconcileQuiescence(runtimeCtx, work.Session)
	cancel()
	if err != nil {
		// Still unknown. The claim stays with this worker and its age has been
		// reset, so the next attempt is an interval away rather than immediate.
		logging.FromContext(ctx).ErrorContext(ctx, "reconcile stranded runtime boundary", "session_id", work.Session.Id, "version", work.Version, "error", err)
		return true
	}
	switch outcome {
	case quiescenceSuspended:
		w.finishQuiescence(ctx, work, snapshot)
	case quiescenceNotSuspended:
		// Admit the session again rather than hold it for work the ordinary
		// claim path can retry.
		if w.settleStranded(ctx, work, w.store.ReleaseSessionQuiescence, "released") {
			// The suspension is owed again, so hint a worker rather than leave
			// it for the next recovery interval. This is a settlement hint like
			// taskstore's, not a handoff: the work this worker held is gone.
			select {
			case w.quiescenceWake <- struct{}{}:
			default:
			}
		}
	case quiescenceGone:
		// No Actor to suspend, now or later. Releasing alone would leave the
		// suspension owed, and the next worker would claim it and fail again.
		w.settleStranded(ctx, work, w.store.RetireSessionQuiescence, "retired")
	default:
		logging.FromContext(ctx).InfoContext(ctx, "stranded runtime boundary is still unsettled", "session_id", work.Session.Id, "version", work.Version)
	}
	return true
}

// settleStranded runs one bounded database call that gives up a reconciled
// claim, so shutdown cannot leave a session closed by a claim this worker
// already knows the answer for.
func (w *ActorWorkflow) settleStranded(ctx context.Context, work *database.SessionQuiescence, settle func(context.Context, *database.SessionQuiescence) error, what string) bool {
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := settle(settleCtx, work); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "settle stranded runtime boundary", "session_id", work.Session.Id, "version", work.Version, "outcome", what, "error", err)
		return false
	}
	logging.FromContext(ctx).InfoContext(ctx, "settled a stranded runtime boundary", "session_id", work.Session.Id, "version", work.Version, "outcome", what)
	return true
}

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
	w.finishQuiescence(ctx, work, snapshot)
}

// finishQuiescence keeps a known snapshot until its reference is stored. It
// retries database failures without repeating runtime work, and gives shutdown
// one bounded completion attempt.
func (w *ActorWorkflow) finishQuiescence(ctx context.Context, work *database.SessionQuiescence, snapshot *database.SessionTaskSnapshot) {
	for delay := 100 * time.Millisecond; ; delay = min(2*delay, 5*time.Second) {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := w.store.FinishSessionQuiescence(finishCtx, work, snapshot)
		finishCancel()
		if err == nil {
			return
		}
		logging.FromContext(ctx).ErrorContext(ctx, "record idle runtime outcome", "session_id", work.Session.Id, "version", work.Version, "error", err)
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
