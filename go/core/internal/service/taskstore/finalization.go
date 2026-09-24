package taskstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/logging"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ manager.Runnable = (*Service)(nil)
var _ manager.LeaderElectionRunnable = (*Service)(nil)

// BoundaryWorkflow owns native pause/snapshot operations. Calls happen after a
// durable claim and outside the store transaction, independently of observers.
type BoundaryWorkflow interface {
	Pause(context.Context, *apiv1alpha1.AgentInstance) error
	Quiesce(context.Context, *apiv1alpha1.AgentInstance) (*database.AgentInstanceTaskSnapshot, error)
}

// Every API replica can finalize work; PostgreSQL grants each boundary once.
func (*Service) NeedLeaderElection() bool { return false }

// Start publishes settled runtime boundaries. Saves wake an idle worker directly;
// a periodic scan also discovers durable work after an API restart. Workers are
// bounded, and a slow snapshot does not serialize unrelated instance saves.
func (s *Service) Start(ctx context.Context) error {
	var workers sync.WaitGroup
	for range cap(s.wake) {
		workers.Go(func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for ctx.Err() == nil {
				work, err := s.store.ClaimTaskFinalization(ctx)
				if err == nil {
					s.finalize(ctx, work)
					continue
				}
				if !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
					logging.FromContext(ctx).ErrorContext(ctx, "claim runtime boundary", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-s.wake:
				case <-timer.C:
				}
			}
		})
	}
	workers.Wait()
	return nil
}

func (s *Service) finalize(ctx context.Context, work *database.TaskFinalization) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var snapshot *database.AgentInstanceTaskSnapshot
	var err error
	if work.State.Terminal() {
		snapshot, err = s.workflow.Quiesce(ctx, work.Instance)
	} else {
		err = s.workflow.Pause(ctx, work.Instance)
	}
	if err != nil {
		// No timeout-based takeover: the Substrate request may still complete.
		// Keep admission closed until its outcome can be safely reconciled.
		logging.FromContext(ctx).ErrorContext(ctx, "runtime boundary outcome unknown", "instance_id", work.Instance.Id, "version", work.Version, "error", err)
		return
	}
	// The native outcome is known. A transient DB failure can retry publication
	// with the same claim and snapshot without issuing another runtime operation.
	for attempt := range 4 {
		if err = s.store.PublishTaskBoundary(ctx, work, snapshot); err == nil {
			return
		}
		if errors.Is(err, database.ErrNotFound) || errors.Is(err, database.ErrConflict) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(1<<attempt) * 100 * time.Millisecond):
		}
	}
	logging.FromContext(ctx).ErrorContext(ctx, "publish runtime boundary", "instance_id", work.Instance.Id, "version", work.Version, "error", err)
}
