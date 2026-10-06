package scheduledrun

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type pollingStore struct {
	controllerStore
	reservations, leases atomic.Int32
	err                  error
}

var _ schedulerStore = (*pollingStore)(nil)
var _ controllerStore = (*pollingStore)(nil)

func (s *pollingStore) ReserveDueScheduledRuns(context.Context, int) error {
	s.reservations.Add(1)
	return s.err
}

func (s *pollingStore) LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error) {
	s.leases.Add(1)
	return nil, s.err
}

func TestScheduledRunWorkersRejectInvalidIntervals(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			require.ErrorContains(t, NewScheduler(nil, interval).Start(t.Context()), "interval must be positive")
			require.ErrorContains(t, NewController(nil, nil, nil, interval).Start(t.Context()), "interval must be positive")
		})
	}
}

func TestConfiguredScheduledRunIntervals(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "no work"},
		{name: "database unavailable", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				store := &pollingStore{err: test.err}
				done := make(chan error, 2)
				go func() { done <- NewScheduler(store, 20*time.Minute).Start(ctx) }()
				go func() { done <- NewController(store, nil, nil, 30*time.Minute).Start(ctx) }()
				synctest.Wait()
				require.EqualValues(t, 1, store.reservations.Load())
				require.EqualValues(t, 1, store.leases.Load())
				time.Sleep(19 * time.Minute)
				synctest.Wait()
				require.EqualValues(t, 1, store.reservations.Load())
				require.EqualValues(t, 1, store.leases.Load())
				time.Sleep(time.Minute)
				synctest.Wait()
				require.EqualValues(t, 2, store.reservations.Load())
				require.EqualValues(t, 1, store.leases.Load())
				time.Sleep(10 * time.Minute)
				synctest.Wait()
				require.EqualValues(t, 2, store.reservations.Load())
				require.EqualValues(t, 2, store.leases.Load())
				cancel()
				for range 2 {
					require.NoError(t, <-done, "shutdown must not wait for the polling interval")
				}
			})
		})
	}
}

type blockedExecutionStore struct {
	controllerStore
	started      chan struct{}
	reservations chan struct{}
}

func (s *blockedExecutionStore) LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error) {
	return []database.LeasedScheduledRunExecution{{Execution: &apiv1alpha1.ScheduledRunExecution{
		Id: "execution", SessionId: "session", Deadline: timestamppb.New(time.Now().Add(time.Minute)),
	}}}, nil
}

func (s *blockedExecutionStore) GetSession(ctx context.Context, _, _ string) (*apiv1alpha1.Session, error) {
	select {
	case s.started <- struct{}{}:
	case <-ctx.Done():
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *blockedExecutionStore) ReserveDueScheduledRuns(ctx context.Context, _ int) error {
	select {
	case s.reservations <- struct{}{}:
	case <-ctx.Done():
	}
	return errors.New("temporary reservation failure")
}

func TestSchedulerTicksWhileExecutionIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &blockedExecutionStore{started: make(chan struct{}), reservations: make(chan struct{})}
	controller := NewController(store, nil, nil, time.Second)
	scheduler := NewScheduler(store, time.Second)
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
