package scheduledrun

import (
	"context"
	"errors"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type blockedExecutionStore struct {
	controllerStore
	started      chan struct{}
	reservations chan struct{}
}

func (s *blockedExecutionStore) LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error) {
	return []database.LeasedScheduledRunExecution{{Execution: &apiv1alpha1.ScheduledRunExecution{
		Id: "execution", AgentInstanceId: "instance", Deadline: timestamppb.New(time.Now().Add(time.Minute)),
	}}}, nil
}

func (s *blockedExecutionStore) GetAgentInstance(ctx context.Context, _, _ string) (*apiv1alpha1.AgentInstance, error) {
	select {
	case s.started <- struct{}{}:
	case <-ctx.Done():
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *blockedExecutionStore) ReserveDueScheduledRuns(ctx context.Context, _ int) ([]*apiv1alpha1.ScheduledRunExecution, error) {
	select {
	case s.reservations <- struct{}{}:
	case <-ctx.Done():
	}
	return nil, errors.New("temporary reservation failure")
}

func TestSchedulerTicksWhileExecutionIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &blockedExecutionStore{started: make(chan struct{}), reservations: make(chan struct{})}
	controller := NewController(store, nil, nil, nil)
	scheduler := NewScheduler(store)
	require.False(t, controller.NeedLeaderElection())
	require.True(t, scheduler.NeedLeaderElection())
	controllerDone := make(chan error, 1)
	go func() { controllerDone <- controller.Start(ctx) }()
	select {
	case <-store.started:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not start")
	}
	schedulerDone := make(chan error, 1)
	go func() { schedulerDone <- scheduler.Start(ctx) }()
	// Both the immediate reservation and the next tick must run while the
	// execution is blocked, even after a reservation failure.
	for range 2 {
		select {
		case <-store.reservations:
		case <-controllerDone:
			t.Fatal("execution controller stopped before cancellation")
		case <-time.After(3 * time.Second):
			t.Fatal("cron reservations stalled behind execution reconciliation")
		}
	}
	cancel()
	for _, done := range []chan error{schedulerDone, controllerDone} {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("runnable did not stop after cancellation")
		}
	}
}
