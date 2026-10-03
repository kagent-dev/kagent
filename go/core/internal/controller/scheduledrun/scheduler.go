package scheduledrun

import (
	"context"
	"fmt"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type schedulerStore interface {
	ReserveDueScheduledRuns(context.Context, int) error
}

// Scheduler reserves due cron firings independently of execution reconciliation.
type Scheduler struct {
	store        schedulerStore
	pollInterval time.Duration
}

var _ manager.LeaderElectionRunnable = (*Scheduler)(nil)
var _ manager.Runnable = (*Scheduler)(nil)

func NewScheduler(store schedulerStore, pollInterval time.Duration) *Scheduler {
	return &Scheduler{store: store, pollInterval: pollInterval}
}

func (*Scheduler) NeedLeaderElection() bool { return true }

func (s *Scheduler) Start(ctx context.Context) error {
	if s.pollInterval <= 0 {
		return fmt.Errorf("scheduled run poll interval must be positive")
	}
	ticker := time.NewTicker(s.pollInterval)
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
