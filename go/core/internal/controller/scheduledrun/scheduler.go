package scheduledrun

import (
	"context"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type schedulerStore interface {
	ReserveDueScheduledRuns(context.Context, int) error
}

// Scheduler reserves due cron firings independently of execution reconciliation.
type Scheduler struct {
	store schedulerStore
}

var _ manager.LeaderElectionRunnable = (*Scheduler)(nil)
var _ manager.Runnable = (*Scheduler)(nil)

func NewScheduler(store schedulerStore) *Scheduler {
	return &Scheduler{store: store}
}

func (*Scheduler) NeedLeaderElection() bool { return true }

func (s *Scheduler) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.store.ReserveDueScheduledRuns(ctx, 100); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reserve scheduled executions", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
