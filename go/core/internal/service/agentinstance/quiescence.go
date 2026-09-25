package agentinstance

import (
	"context"
	"errors"
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

// Start pauses or suspends idle instances independently of task publication.
// A periodic scan discovers settled work across API replicas and restarts.
// Workers are bounded; task reads never wait for them.
func (w *ActorWorkflow) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for ctx.Err() == nil {
				work, err := w.store.ClaimInstanceQuiescence(ctx)
				if err == nil {
					w.quiesceIdleInstance(ctx, work)
					continue
				}
				if !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
					logging.FromContext(ctx).ErrorContext(ctx, "claim runtime boundary", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		})
	}
	workers.Wait()
	return nil
}

func (w *ActorWorkflow) quiesceIdleInstance(ctx context.Context, work *database.InstanceQuiescence) {
	runtimeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	var snapshot *database.AgentInstanceTaskSnapshot
	var err error
	if work.State.Terminal() {
		snapshot, err = w.Quiesce(runtimeCtx, work.Instance)
	} else {
		err = w.Pause(runtimeCtx, work.Instance)
	}
	cancel()
	if err != nil {
		// No timeout-based takeover: the Substrate request may still complete.
		// Keep admission closed until its outcome can be safely reconciled.
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary outcome unknown", "instance_id", work.Instance.Id, "version", work.Version, "error", err)
		return
	}
	// Keep a known snapshot until its reference is stored. Retry database failures
	// without repeating runtime work; give shutdown one bounded completion attempt.
	for delay := 100 * time.Millisecond; ; delay = min(2*delay, 5*time.Second) {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = w.store.FinishInstanceQuiescence(finishCtx, work, snapshot)
		finishCancel()
		if err == nil {
			return
		}
		logging.FromContext(ctx).ErrorContext(ctx, "record idle runtime outcome", "instance_id", work.Instance.Id, "version", work.Version, "error", err)
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
