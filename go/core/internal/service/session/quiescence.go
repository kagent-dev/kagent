package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
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
// A claim's holder renews its lease until the claim is settled, so the claim of
// a replica that stopped is taken over by another and settled there. A pause
// older than the paused runtime TTL is claimed again for a suspend, so a reply
// that outlives the pause's node restores the runtime from its external
// snapshot instead.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			timer := time.NewTimer(w.recoveryInterval)
			defer timer.Stop()
			for ctx.Err() == nil {
				work, err := w.store.ClaimSessionQuiescence(ctx, w.claimLease, w.pausedRuntimeTTL, w.deferred.active(time.Now()))
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
	w.settling.Wait()
	return nil
}

// quiesceIdleSession holds the claim's lease until its boundary is settled. A
// claim taken over from a stopped holder is settled from the Actor's state.
func (w *ActorWorkflow) quiesceIdleSession(ctx context.Context, work *database.SessionQuiescence) {
	release := w.holdClaim(ctx, work)
	if work.TakenOver {
		logging.FromContext(ctx).WarnContext(ctx, "runtime boundary taken over from a stopped holder", "session_id", work.Session.Id, "task_id", work.TaskID, "version", work.Version)
	} else if w.issueIdleWork(ctx, work) {
		release()
		return
	}
	w.settling.Go(func() {
		defer release()
		w.settleIdleWork(ctx, work)
	})
}

// holdClaim renews the claim's lease every third of it until the returned
// function is called. Stopping the process stops the renewal, and another
// worker takes the claim over once the lease has run out.
func (w *ActorWorkflow) holdClaim(ctx context.Context, work *database.SessionQuiescence) func() {
	holdCtx, cancel := context.WithCancel(ctx)
	var renewing sync.WaitGroup
	renewing.Go(func() {
		ticker := time.NewTicker(w.claimLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-holdCtx.Done():
				return
			case <-ticker.C:
			}
			renewCtx, renewCancel := context.WithTimeout(holdCtx, w.claimLease/3)
			err := w.store.RenewSessionQuiescence(renewCtx, work, w.claimLease)
			renewCancel()
			switch {
			case err == nil, holdCtx.Err() != nil:
			case errors.Is(err, database.ErrNotFound):
				return
			default:
				logging.FromContext(ctx).WarnContext(ctx, "renew runtime boundary claim", "session_id", work.Session.Id, "version", work.Version, "error", err)
			}
		}
	})
	return func() {
		cancel()
		renewing.Wait()
	}
}

// issueIdleWork pauses or suspends the claimed session and records the outcome.
// It reports false when the runtime request's outcome is unknown and the claim
// is left for settleIdleWork. The request carries the holder's fencing token:
// a holder frozen past its lease, whose claim another worker took over and
// settled, finds its late request refused by Substrate and leaves the claim to
// its successor.
func (w *ActorWorkflow) issueIdleWork(ctx context.Context, work *database.SessionQuiescence) bool {
	runtimeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	var snapshot *database.SessionTaskSnapshot
	runtimeCtx, err := w.fence(runtimeCtx, work)
	if err != nil {
		cancel()
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary not issued", "session_id", work.Session.Id, "version", work.Version, "error", err)
		return false
	}
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
			return true
		}
	}
	if work.State.Terminal() || work.Suspend {
		snapshot, err = w.Quiesce(runtimeCtx, work.Session)
	} else {
		err = w.Pause(runtimeCtx, work.Session)
	}
	cancel()
	if status.Code(err) == codes.FailedPrecondition {
		// Substrate holds a newer fencing token: this claim was taken over and
		// its successor settles it. Renewal stops; should the refusal have had
		// another cause, the lease runs out and the claim is taken over as well.
		logging.FromContext(ctx).WarnContext(ctx, "runtime boundary superseded by a newer holder", "session_id", work.Session.Id, "task_id", work.TaskID, "version", work.Version, "error", err)
		return true
	}
	if err != nil {
		// No timeout-based takeover: the Substrate request may still complete.
		// Keep admission closed until the Actor's own state settles the outcome.
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary outcome unknown", "session_id", work.Session.Id, "task_id", work.TaskID, "version", work.Version, "error", err)
		return false
	}
	w.finishIdleWork(ctx, work, snapshot)
	return true
}

// settleIdleWork resolves a claim whose Pause or Quiesce failed from the state
// Substrate reports for the Actor, so the session does not refuse sends forever.
// A suspended Actor's external snapshot finishes the claim as Quiesce would
// have, and a paused Actor finishes a pause. While the Actor is still in
// transition, or Substrate cannot answer, the claim is kept and read again with
// a backoff. Any other state, a running, crashed or missing Actor included,
// releases the claim without a snapshot: the next send takes the runtime as it
// is, and a lost one fails the session there; a running Actor is fenced first.
// A worker that stops while it settles leaves the claim to another once the
// lease has run out.
func (w *ActorWorkflow) settleIdleWork(ctx context.Context, work *database.SessionQuiescence) {
	log := logging.FromContext(ctx).With("session_id", work.Session.Id, "task_id", work.TaskID, "version", work.Version)
	for delay := w.settleDelay; ; delay = min(2*delay, time.Minute) {
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		actor, snapshot, err := w.boundaryActor(readCtx, work.Session)
		cancel()
		state := actor.GetStatus().GetState()
		switch {
		case err != nil:
			log.WarnContext(ctx, "runtime boundary still unknown", "error", err, "retry_in", delay)
		case actor != nil && inTransition(state):
			log.InfoContext(ctx, "runtime boundary still in transition", "actor_state", state, "retry_in", delay)
		case snapshot != nil:
			log.InfoContext(ctx, "runtime boundary settled from suspended Actor", "snapshot_uri", snapshot.URI)
			w.finishIdleWork(ctx, work, snapshot)
			return
		case state == ateapipb.ActorState_ACTOR_STATE_PAUSED && !work.State.Terminal() && !work.Suspend:
			log.InfoContext(ctx, "runtime boundary settled from paused Actor")
			w.finishIdleWork(ctx, work, nil)
			return
		default:
			if w.releaseIdleWork(ctx, log, work, actor, delay) {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// releaseIdleWork releases a claim whose Actor was neither suspended nor
// paused. A running Actor first gets a fencing token newer than any request
// this claim's holders sent, by a fenced Resume that leaves the runtime as it
// is, so a Pause or Suspend still in flight cannot land after the release. It
// reports false when the fence could not be set yet and is tried again.
func (w *ActorWorkflow) releaseIdleWork(ctx context.Context, log *slog.Logger, work *database.SessionQuiescence, actor *ateapipb.Actor, delay time.Duration) bool {
	state := actor.GetStatus().GetState()
	if state == ateapipb.ActorState_ACTOR_STATE_RUNNING {
		err := w.fenceRunning(ctx, work, actor)
		if status.Code(err) == codes.FailedPrecondition {
			log.WarnContext(ctx, "runtime boundary superseded by a newer holder", "error", err)
			return true
		}
		if err != nil {
			log.WarnContext(ctx, "fence running Actor", "error", err, "retry_in", delay)
			return false
		}
	}
	log.WarnContext(ctx, "runtime boundary released without snapshot", "actor_state", state, "actor_found", actor != nil)
	if work.Suspend {
		w.deferred.add(work.Session.Id, time.Now().Add(w.pausedRuntimeTTL))
	}
	w.recordIdleWork(ctx, work, func(ctx context.Context) error { return w.store.ReleaseSessionQuiescence(ctx, work) })
	return true
}

// fenceRunning records a new fencing token of work's holder on the running
// Actor without touching the runtime: Substrate's Resume of a running Actor
// only records a newer token.
func (w *ActorWorkflow) fenceRunning(ctx context.Context, work *database.SessionQuiescence, actor *ateapipb.Actor) error {
	fenceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fenceCtx, err := w.fence(fenceCtx, work)
	if err != nil {
		return err
	}
	atespace, name := actor.GetMetadata().GetAtespace(), actor.GetMetadata().GetName()
	resumed, err := w.actors.ResumeActor(fenceCtx, atespace, name)
	if err != nil {
		return fmt.Errorf("fence Actor %s/%s: %w", atespace, name, err)
	}
	if state := resumed.GetStatus().GetState(); state != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		return fmt.Errorf("fence Actor %s/%s returned status %s", atespace, name, state)
	}
	return nil
}

// fence returns ctx carrying a fencing token of work's holder whose generation
// is newer than that of every request sent before, by any holder.
func (w *ActorWorkflow) fence(ctx context.Context, work *database.SessionQuiescence) (context.Context, error) {
	generation, err := w.store.NextQuiescenceFencingGeneration(ctx)
	if err != nil {
		return ctx, fmt.Errorf("next fencing generation: %w", err)
	}
	return substrate.WithFencingToken(ctx, &ateapipb.FencingToken{Holder: work.ExecutorID.String(), Generation: generation}), nil
}

func inTransition(state ateapipb.ActorState) bool {
	switch state {
	case ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED, ateapipb.ActorState_ACTOR_STATE_RESUMING, ateapipb.ActorState_ACTOR_STATE_SUSPENDING,
		ateapipb.ActorState_ACTOR_STATE_PAUSING, ateapipb.ActorState_ACTOR_STATE_DELETING, ateapipb.ActorState_ACTOR_STATE_REVERTING:
		return true
	}
	return false
}

// finishIdleWork records the claim's outcome. Keep a known snapshot until its
// reference is stored.
func (w *ActorWorkflow) finishIdleWork(ctx context.Context, work *database.SessionQuiescence, snapshot *database.SessionTaskSnapshot) {
	w.recordIdleWork(ctx, work, func(ctx context.Context) error { return w.store.FinishSessionQuiescence(ctx, work, snapshot) })
}

// recordIdleWork retries database failures without repeating runtime work;
// give shutdown one bounded completion attempt.
func (w *ActorWorkflow) recordIdleWork(ctx context.Context, work *database.SessionQuiescence, record func(context.Context) error) {
	var err error
	for delay := 100 * time.Millisecond; ; delay = min(2*delay, 5*time.Second) {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = record(finishCtx)
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
