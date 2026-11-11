package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

var _ manager.Runnable = (*ActorWorkflow)(nil)
var _ manager.LeaderElectionRunnable = (*ActorWorkflow)(nil)

// quiescenceWork is either a recovery scan cursor or one exclusively owned
// dispatch. Its stable queue identity prevents concurrent attempts by workers.
// Workers own the cursor and trigger; wakeups only mark the scan for another pass.
type quiescenceWork struct {
	afterSessionID string
	trigger        workTrigger
	// A shared flag attributes one claim to this wake batch; nil means no wake.
	signal  atomic.Pointer[atomic.Bool]
	cleanup *dispatchCleanup
}

// quiescenceQueue keeps one scan per execution worker. Task claims execute on
// that worker immediately; only dispatch cleanup, whose claim expires, is queued.
type quiescenceQueue struct {
	workqueue.TypedDelayingInterface[*quiescenceWork]
	scans [4]*quiescenceWork
}

// newQuiescenceQueue gives each worker an independent scan cursor.
func newQuiescenceQueue() *quiescenceQueue {
	queue := &quiescenceQueue{TypedDelayingInterface: workqueue.NewTypedDelayingQueue[*quiescenceWork]()}
	for i := range queue.scans {
		queue.scans[i] = &quiescenceWork{trigger: workStartup}
		queue.Add(queue.scans[i])
	}
	return queue
}

// wake enlists every available worker. The first successful claim consumes the
// shared signal; other claims count as backlog. An empty scan cannot steal it.
func (q *quiescenceQueue) wake(trigger workTrigger) {
	signal := new(atomic.Bool)
	signal.Store(trigger == workSignal)
	for _, scan := range q.scans {
		if trigger == workSignal {
			scan.signal.Store(signal)
		} else {
			// A worker handoff must not overwrite an unconsumed settlement.
			scan.signal.CompareAndSwap(nil, signal)
		}
		q.Add(scan)
	}
}

// Every API replica can process idle work; PostgreSQL grants each claim once.
func (*ActorWorkflow) NeedLeaderElection() bool { return false }

// Start suspends idle sessions to VOLUMES independently of task publication.
// Startup and periodic recovery scans discover work missed by local signals.
// Workers drain claims with bounded concurrency; task reads never wait for them.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	worker, err := w.newQuiescenceWorker(ctx)
	if err != nil {
		return err
	}
	queue := newQuiescenceQueue()
	var workers sync.WaitGroup
	for range queue.scans {
		workers.Go(func() {
			for {
				work, shutdown := queue.Get()
				if shutdown {
					return
				}
				if ctx.Err() == nil {
					worker.processQuiescence(ctx, queue, work)
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
			queue.wake(workSignal)
		}
	}
}

// processQuiescence yields between claimed attempts so failing actors cannot
// occupy every worker indefinitely. Scan tokens share the same fair queue.
func (w *ActorWorkflow) processQuiescence(ctx context.Context, queue *quiescenceQueue, work *quiescenceWork) {
	if work.cleanup == nil {
		w.scanQuiescence(ctx, queue, work)
		return
	}
	done, delay, err := w.retryDispatchCleanup(ctx, work.cleanup)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "abandoned dispatch suspension outcome unknown", "session_id", work.cleanup.work.Session.Id, "dispatch_id", work.cleanup.work.DispatchID, "error", err)
	}
	if !done {
		queue.AddAfter(work, delay)
	}
}

// scanQuiescence advances one scan cursor and schedules its next pass. Dispatch
// claims enter the queue; ordinary task claims execute on this worker immediately.
func (w *ActorWorkflow) scanQuiescence(ctx context.Context, queue *quiescenceQueue, work *quiescenceWork) {
	nextSessionID, cleanup, err := w.quiesceNextSession(ctx, queue, work)
	work.afterSessionID = nextSessionID
	if cleanup != nil {
		queue.Add(&quiescenceWork{cleanup: cleanup})
	}
	if err == nil {
		work.trigger = workBacklog
		queue.Add(work)
		return
	}
	delay := min(time.Second, w.recoveryInterval)
	work.trigger = workRetry
	if errors.Is(err, database.ErrNotFound) {
		pending, pendingErr := w.store.HasPendingSessionQuiescence(ctx)
		if pendingErr != nil {
			err = pendingErr
		} else if !pending {
			delay = w.recoveryInterval
			work.trigger = workRecovery
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
func (w *ActorWorkflow) quiesceNextSession(ctx context.Context, queue *quiescenceQueue, scan *quiescenceWork) (string, *dispatchCleanup, error) {
	afterSessionID, trigger := scan.afterSessionID, scan.trigger
	signal := scan.signal.Swap(nil)
	cleanup, err := w.claimAbandonedDispatch(ctx, afterSessionID)
	if err == nil {
		if signal != nil {
			signal.Store(false)
		}
		// Visit each candidate once per scan. An unfinished restore must not
		// busy-loop or hide later Sessions while it waits for the next scan.
		return cleanup.work.Session.Id, cleanup, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return afterSessionID, nil, err
	}
	work, err := w.store.ClaimSessionQuiescence(ctx)
	if err != nil {
		return afterSessionID, nil, err
	}
	if signal != nil {
		trigger = workBacklog
		if signal.Swap(false) {
			trigger = workSignal
		}
	}
	queue.wake(workBacklog)
	recordWorkClaim(ctx, w.quiescenceClaims, trigger)
	trigger.logRecoveryClaim(ctx, operationSessionQuiescence, w.recoveryInterval, slog.String("session_id", work.Session.GetId()))
	w.quiesceIdleSession(ctx, work)
	return afterSessionID, nil, nil
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
