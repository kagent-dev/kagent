package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type schedulerStore interface {
	ListDueScheduledRuns(context.Context, []string, uuid.UUID, int) ([]database.ScheduledRunSource, error)
	ReserveDueScheduledRuns(context.Context, []database.ScheduledRunSource) error
	RetireScheduledRun(context.Context, uuid.UUID) error
}

// Scheduler reserves due cron firings independently of execution reconciliation.
type Scheduler struct {
	store        schedulerStore
	reader       client.Reader
	namespaces   []string
	pollInterval time.Duration
}

var _ manager.LeaderElectionRunnable = (*Scheduler)(nil)
var _ manager.Runnable = (*Scheduler)(nil)

func NewScheduler(store schedulerStore, reader client.Reader, namespaces []string, pollInterval time.Duration) *Scheduler {
	return &Scheduler{store: store, reader: reader, namespaces: slices.Clone(namespaces), pollInterval: pollInterval}
}

func (*Scheduler) NeedLeaderElection() bool { return true }

func (s *Scheduler) Start(ctx context.Context) error {
	if s.pollInterval <= 0 {
		return fmt.Errorf("scheduled run poll interval must be positive")
	}
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		if err := s.reserveDue(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reserve scheduled executions", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) reserveDue(ctx context.Context) error {
	var failures []error
	afterID := uuid.Nil
	for {
		sources, err := s.store.ListDueScheduledRuns(ctx, s.namespaces, afterID, 100)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if err := s.reserveBatch(ctx, sources); err != nil {
			failures = append(failures, err)
		}
		if len(sources) < 100 {
			return errors.Join(failures...)
		}
		afterID = sources[len(sources)-1].ID
	}
}

func (s *Scheduler) reserveBatch(ctx context.Context, sources []database.ScheduledRunSource) error {
	valid := make([]database.ScheduledRunSource, 0, len(sources))
	var failures []error
	for _, source := range sources {
		resource := &v1alpha3.ScheduledRun{}
		ref := types.NamespacedName{Namespace: source.Namespace, Name: source.Name}
		err := s.reader.Get(ctx, ref, resource)
		if err != nil && !apierrors.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("read ScheduledRun %s: %w", ref, err))
			continue
		}
		if apierrors.IsNotFound(err) || string(resource.UID) != source.ID.String() || !resource.DeletionTimestamp.IsZero() {
			if err := s.store.RetireScheduledRun(ctx, source.ID); err != nil {
				failures = append(failures, fmt.Errorf("retire ScheduledRun %s: %w", source.ID, err))
			}
			continue
		}
		if resource.Generation == source.Generation {
			valid = append(valid, source)
		}
	}
	// The store rechecks generations and eligibility under row locks, fencing
	// reconciliation between the source read and reservation. Accepted executions
	// retain their snapshots even if Kubernetes deletes the source afterwards.
	if err := s.store.ReserveDueScheduledRuns(ctx, valid); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
