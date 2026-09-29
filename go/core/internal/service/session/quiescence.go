package session

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/logging"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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

// Start pauses or suspends idle sessions independently of task publication.
// Startup and periodic recovery scans discover work missed by local signals.
// Workers drain claims with bounded concurrency; task reads never wait for them.
// A pause older than the paused runtime TTL is claimed again for a suspend, so a
// reply that outlives the pause's node restores the runtime from its external
// snapshot instead.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			timer := time.NewTimer(w.recoveryInterval)
			defer timer.Stop()
			for ctx.Err() == nil {
				work, err := w.store.ClaimSessionQuiescence(ctx, w.pausedRuntimeTTL, w.deferred.active(time.Now()))
				if err == nil {
					// Hand off a hint before runtime I/O so a single settlement
					// burst can enlist all workers, even if this actor is slow.
					w.wakeQuiescence()
					w.quiesceIdleSession(ctx, work)
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
			}
		})
	}
	workers.Wait()
	return nil
}

func (w *ActorWorkflow) quiesceIdleSession(ctx context.Context, work *database.SessionQuiescence) {
	runtimeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	var snapshot *database.SessionTaskSnapshot
	var err error
	if work.Suspend {
		// Nothing has been issued yet, so a suspend that cannot happen now
		// releases the claim, reopening admission for the reply, and the
		// session is left out of the next claims for one TTL.
		lost, err := w.PauseNodeLost(runtimeCtx, work.Session)
		if err != nil || lost {
			cancel()
			if err != nil {
				logging.FromContext(ctx).WarnContext(ctx, "expired pause left in place", "session_id", work.Session.Id, "version", work.Version, "error", err)
			} else {
				logging.FromContext(ctx).InfoContext(ctx, "expired pause left to node-loss handling", "session_id", work.Session.Id, "version", work.Version)
			}
			w.deferred.add(work.Session.Id, time.Now().Add(w.pausedRuntimeTTL))
			w.finishIdleWork(ctx, work, nil)
			return
		}
	}
	if work.State.Terminal() || work.Suspend {
		snapshot, err = w.Quiesce(runtimeCtx, work.Session)
	} else {
		err = w.Pause(runtimeCtx, work.Session)
	}
	cancel()
	if err != nil {
		// No timeout-based takeover: the Substrate request may still complete.
		// Keep admission closed until its outcome can be safely reconciled.
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary outcome unknown", "session_id", work.Session.Id, "version", work.Version, "error", err)
		return
	}
	w.finishIdleWork(ctx, work, snapshot)
}

// finishIdleWork records the claim's outcome. Keep a known snapshot until its
// reference is stored. Retry database failures without repeating runtime work;
// give shutdown one bounded completion attempt.
func (w *ActorWorkflow) finishIdleWork(ctx context.Context, work *database.SessionQuiescence, snapshot *database.SessionTaskSnapshot) {
	var err error
	for delay := 100 * time.Millisecond; ; delay = min(2*delay, 5*time.Second) {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = w.store.FinishSessionQuiescence(finishCtx, work, snapshot)
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

// deferrals holds sessions the idle worker leaves out of the pause TTL's claims
// until a time, so a pause that cannot be suspended is not claimed and released
// every second by every worker.
type deferrals struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (d *deferrals) add(sessionID string, until time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.until == nil {
		d.until = map[string]time.Time{}
	}
	d.until[sessionID] = until
}

// active returns the sessions still deferred at now and forgets the others.
func (d *deferrals) active(now time.Time) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for id, until := range d.until {
		if now.Before(until) {
			ids = append(ids, id)
		} else {
			delete(d.until, id)
		}
	}
	slices.Sort(ids)
	return ids
}
