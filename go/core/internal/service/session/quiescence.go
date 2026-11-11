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
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

var _ manager.Runnable = (*ActorWorkflow)(nil)
var _ manager.LeaderElectionRunnable = (*ActorWorkflow)(nil)

const dispatchCleanupMaxRetryDelay = 15 * time.Second

// quiescenceWork is either a recovery scan cursor or one exclusively owned
// dispatch. Its stable queue identity prevents concurrent attempts by workers.
type quiescenceWork struct {
	afterSessionID string
	cleanup        *dispatchCleanup
}

// dispatchCleanup retains the owner and completed phases between bounded
// attempts. A finish callback records a known outcome without more actor calls.
type dispatchCleanup struct {
	work              *database.SessionDispatchCleanup
	revision          *database.RuntimeRevision
	suspending        bool
	ambiguousMutation bool
	finish            func(context.Context) error
	retryDelay        time.Duration
}

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
	queue := workqueue.NewTypedDelayingQueue[*quiescenceWork]()
	scans := make([]*quiescenceWork, 4)
	var workers sync.WaitGroup
	for i := range scans {
		scans[i] = &quiescenceWork{}
		queue.Add(scans[i])
		workers.Go(func() {
			for {
				work, shutdown := queue.Get()
				if shutdown {
					return
				}
				if ctx.Err() == nil {
					w.processQuiescence(ctx, queue, work)
				}
				queue.Done(work)
			}
		})
	}
	for {
		select {
		case <-ctx.Done():
			queue.ShutDown()
			workers.Wait()
			return nil
		case <-w.quiescenceWake:
			for _, scan := range scans {
				queue.Add(scan)
			}
		}
	}
}

// processQuiescence yields between claimed attempts so failing actors cannot
// occupy every worker indefinitely. Scan tokens share the same fair queue.
func (w *ActorWorkflow) processQuiescence(ctx context.Context, queue workqueue.TypedDelayingInterface[*quiescenceWork], work *quiescenceWork) {
	if work.cleanup != nil {
		done, delay, err := w.retryDispatchCleanup(ctx, work.cleanup)
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "abandoned dispatch suspension outcome unknown", "session_id", work.cleanup.work.Session.Id, "dispatch_id", work.cleanup.work.DispatchID, "error", err)
		}
		if !done {
			queue.AddAfter(work, delay)
		}
		return
	}
	nextSessionID, cleanup, err := w.quiesceNextSession(ctx, work.afterSessionID)
	work.afterSessionID = nextSessionID
	if cleanup != nil {
		queue.Add(&quiescenceWork{cleanup: cleanup})
	}
	if err == nil {
		queue.Add(work)
		return
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
	work.afterSessionID = ""
	queue.AddAfter(work, delay)
}

// quiesceNextSession processes one abandoned delivery or task awaiting suspension.
// It returns the scan cursor and any claimed dispatch for queued execution;
// ErrNotFound means neither has work.
func (w *ActorWorkflow) quiesceNextSession(ctx context.Context, afterSessionID string) (string, *dispatchCleanup, error) {
	cleanup, err := w.store.GetPendingSessionDispatchCleanup(ctx, afterSessionID)
	if err == nil {
		// Visit each candidate once per scan. An unfinished restore must not
		// busy-loop or hide later Sessions while it waits for the next scan.
		claimed, err := w.claimAbandonedDispatch(ctx, cleanup)
		return cleanup.Session.Id, claimed, err
	}
	if !errors.Is(err, database.ErrNotFound) {
		return afterSessionID, nil, err
	}
	work, err := w.store.ClaimSessionQuiescence(ctx)
	if err != nil {
		return afterSessionID, nil, err
	}
	// Hand off a hint before runtime I/O so a burst can enlist all workers.
	w.wakeQuiescence()
	w.quiesceIdleSession(ctx, work)
	return afterSessionID, nil, nil
}

// claimAbandonedDispatch verifies the actor and claims abandoned delivery before
// queueing runtime work. The claim prevents new execution through all attempts.
func (w *ActorWorkflow) claimAbandonedDispatch(ctx context.Context, candidate *database.SessionDispatchCleanup) (*dispatchCleanup, error) {
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
		return nil, nil
	}
	state := actor.GetStatus().GetState()
	if state != ateapipb.ActorState_ACTOR_STATE_RUNNING && state != ateapipb.ActorState_ACTOR_STATE_RESUMING {
		// SUSPENDED is not proof that canceled ingress cannot resume later.
		return nil, nil
	}
	work, err := w.store.ClaimSessionDispatchCleanup(runtimeCtx, candidate)
	cancel()
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
		return nil, nil // A new send or another lifecycle worker won admission.
	}
	if err != nil {
		return nil, err
	}
	w.wakeQuiescence()
	return &dispatchCleanup{work: work, revision: revision}, nil
}

// retryDispatchCleanup performs one bounded runtime or persistence attempt.
// A retry retains the same claim and completed phases but yields its worker.
// Terminal errors leave the claim fenced; only force deletion may supersede it.
func (w *ActorWorkflow) retryDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup) (done bool, delay time.Duration, err error) {
	if err := ctx.Err(); err != nil && cleanup.finish == nil {
		return true, 0, err
	}
	retryable := true
	if cleanup.finish == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
		current, readErr := w.store.GetSessionForRuntime(attemptCtx, cleanup.work.Session.Id, cleanup.work.ActorUID)
		err = readErr
		if errors.Is(err, database.ErrNotFound) || (err == nil && !lifecycleIdle(current)) {
			cancel()
			// Force deletion may supersede this claim. It never permits a new
			// turn or recreates the actor, even if an old call is still in flight.
			return true, 0, nil
		}
		if err == nil {
			err = w.advanceDispatchCleanup(attemptCtx, cleanup)
			retryable = retryableCleanupCall(err)
		}
		cancel()
	}
	if err == nil && cleanup.finish != nil {
		// Attempt to persist a known outcome before yielding, including on
		// shutdown. Failed writes requeue without repeating runtime work.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = cleanup.finish(finishCtx)
		cancel()
		if err == nil {
			return true, 0, nil
		}
		retryable = !errors.Is(err, database.ErrNotFound) && !errors.Is(err, database.ErrConflict)
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
	logging.FromContext(ctx).DebugContext(ctx, "retry abandoned dispatch cleanup", "session_id", cleanup.work.Session.Id, "dispatch_id", cleanup.work.DispatchID, "error", err)
	if cleanup.retryDelay == 0 {
		cleanup.retryDelay = 100 * time.Millisecond
	} else {
		cleanup.retryDelay = min(2*cleanup.retryDelay, dispatchCleanupMaxRetryDelay)
	}
	return false, cleanup.retryDelay, nil
}

// advanceDispatchCleanup chooses the first action from a fresh owned observation.
// Once resume succeeds or suspension starts, it never resumes again. A known
// terminal outcome switches permanently to database completion or owner release.
func (w *ActorWorkflow) advanceDispatchCleanup(ctx context.Context, cleanup *dispatchCleanup) error {
	work, revision := cleanup.work, cleanup.revision
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(work.Session.Id)
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return err
	}
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != work.ActorUID {
		return fmt.Errorf("runtime Actor changed during abandoned dispatch cleanup")
	}
	state := actor.GetStatus().GetState()
	previousSuspension := !cleanup.suspending && (state == ateapipb.ActorState_ACTOR_STATE_SUSPENDING || state == ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
	if state == ateapipb.ActorState_ACTOR_STATE_CRASHED || previousSuspension {
		if cleanup.ambiguousMutation {
			return fmt.Errorf("runtime Actor reached %s after an uncertain cleanup call", state)
		}
		// Another suspension may finish between preparation and claiming. Keep
		// the expired dispatch so a later canceled-ingress resume is still found.
		// A confirmed crash instead permits recovery once this owner stops.
		cleanup.finish = func(finishCtx context.Context) error {
			return w.store.ReleaseSessionDispatchCleanup(finishCtx, work)
		}
		return nil
	}
	if !cleanup.suspending && state == ateapipb.ActorState_ACTOR_STATE_RESUMING {
		err = w.resumeQuiescentActor(ctx, work.Session, revision, actor)
		if err == nil {
			cleanup.suspending = true
		}
	} else {
		if state != ateapipb.ActorState_ACTOR_STATE_RUNNING && state != ateapipb.ActorState_ACTOR_STATE_SUSPENDING && state != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
			return fmt.Errorf("runtime Actor cannot continue abandoned dispatch cleanup in state %s", state)
		}
		cleanup.suspending = true
		var snapshot *database.SessionTaskSnapshot
		snapshot, err = w.suspendQuiescentActor(ctx, work.Session, revision, actor)
		if err == nil {
			cleanup.finish = func(finishCtx context.Context) error {
				return w.store.FinishSessionDispatchCleanup(finishCtx, work, snapshot)
			}
		}
	}
	// A later successful Resume may use Substrate's RUNNING fast path;
	// it cannot prove an earlier timed-out mutation finished remotely.
	if ctx.Err() != nil || !conclusiveCleanupCall(err) {
		cleanup.ambiguousMutation = true
	}
	return err
}

// conclusiveCleanupCall identifies completed Substrate replies, not absence of
// effects. It must never erase uncertainty from an earlier mutation attempt.
func conclusiveCleanupCall(err error) bool {
	switch status.Code(err) {
	case codes.OK, codes.Aborted, codes.FailedPrecondition:
		return true
	default:
		return false
	}
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

// recordQuiescence retries recording a known outcome without further actor calls.
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
