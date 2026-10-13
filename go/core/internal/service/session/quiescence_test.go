package session

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type idleQuiescenceStore struct {
	workflowStore
	claims        atomic.Int32
	pendingChecks atomic.Int32
	pending       atomic.Bool
	claimErr      error
	pendingErr    error
	beforeIdle    func()
}

var _ workflowStore = (*idleQuiescenceStore)(nil)

func (s *idleQuiescenceStore) ClaimSessionQuiescence(context.Context) (*database.SessionQuiescence, error) {
	s.claims.Add(1)
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return nil, database.ErrNotFound
}

func (s *idleQuiescenceStore) HasPendingSessionQuiescence(context.Context) (bool, error) {
	s.pendingChecks.Add(1)
	if s.beforeIdle != nil {
		s.beforeIdle()
	}
	return s.pending.Load(), s.pendingErr
}

func TestConfiguredQuiescenceInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &idleQuiescenceStore{}
		done := make(chan error, 1)
		go func() { done <- NewActorWorkflow(store, nil, make(chan struct{}, 1), 20*time.Minute).Start(ctx) }()
		synctest.Wait()
		require.EqualValues(t, 4, store.claims.Load())
		require.EqualValues(t, 4, store.pendingChecks.Load())
		time.Sleep(19 * time.Minute)
		synctest.Wait()
		require.EqualValues(t, 4, store.claims.Load(), "idle workers must not query before the interval")
		time.Sleep(time.Minute)
		synctest.Wait()
		require.EqualValues(t, 8, store.claims.Load())
		cancel()
		require.NoError(t, <-done, "shutdown must not wait for the polling interval")
	})
}

func TestQuiescenceWakeRacesIdleScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &idleQuiescenceStore{}
		workflow := NewActorWorkflow(store, nil, make(chan struct{}, 1), time.Hour)
		var once sync.Once
		store.beforeIdle = func() { once.Do(workflow.wakeQuiescence) }
		done := make(chan error, 1)
		go func() { done <- workflow.Start(ctx) }()
		synctest.Wait()
		require.EqualValues(t, 5, store.claims.Load(), "a signal during an empty scan must trigger another claim")
		var senders sync.WaitGroup
		for range 100 {
			senders.Go(workflow.wakeQuiescence)
		}
		senders.Wait()
		synctest.Wait()
		require.Greater(t, store.claims.Load(), int32(5))
		cancel()
		require.NoError(t, <-done)
		for range 100 {
			workflow.wakeQuiescence() // Shutdown cannot block a committing caller.
		}
	})
}

func TestQuiescenceRetriesPendingWorkAndDatabaseErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		pending    bool
		claimErr   error
		pendingErr error
	}{
		{name: "blocked work", pending: true},
		{name: "claim failed", claimErr: status.Error(codes.Unavailable, "offline")},
		{name: "pending check failed", pendingErr: status.Error(codes.Unavailable, "offline")},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				store := &idleQuiescenceStore{claimErr: test.claimErr, pendingErr: test.pendingErr}
				store.pending.Store(test.pending)
				done := make(chan error, 1)
				go func() { done <- NewActorWorkflow(store, nil, make(chan struct{}, 1), time.Hour).Start(ctx) }()
				synctest.Wait()
				require.EqualValues(t, 4, store.claims.Load())
				time.Sleep(time.Second)
				synctest.Wait()
				require.EqualValues(t, 8, store.claims.Load(), "known pending work and errors must not wait for recovery")
				cancel()
				require.NoError(t, <-done)
			})
		})
	}
}

func TestIdleLifecycleDoesNotOwnTaskPublication(t *testing.T) {
	for _, test := range []struct {
		name           string
		mutationFails  bool
		finishFailures int32
	}{
		{name: "snapshot succeeds"},
		{name: "snapshot outcome unknown", mutationFails: true},
		{name: "snapshot reference survives database retries", finishFailures: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			message.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			task.Status.State = a2a.TaskStateCompleted
			hash := sha256.Sum256([]byte("completed"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
			require.NoError(t, err)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))

			entered, release := make(chan struct{}), make(chan struct{})
			actors := &retryTestActors{lifecycleTestActors: base, beforeRead: func(ctx context.Context) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}}
			if test.mutationFails {
				actors.mutationErr = status.Error(codes.Unavailable, "lost suspend response")
			}
			// A new lifecycle worker discovers durable idle work without any
			// notification or participation from the task persistence service.
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			writes := &quiescenceRetryStore{lifecycleTestStore: store, failures: test.finishFailures}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour)
			go func() { done <- workflow.Start(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("idle work was not discovered")
			}
			visible, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateCompleted, visible.Status.State)
			next := a2a.NewSubmittedTask(message, message)
			_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], next, "")
			require.ErrorIs(t, err, database.ErrFailedPrecondition)
			checkpoint := &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
			_, _, err = store.ReserveSessionCheckpoint(t.Context(), checkpoint, "alice", "checkpoint")
			require.ErrorIs(t, err, database.ErrFailedPrecondition)
			close(release)

			if test.mutationFails {
				require.Eventually(t, func() bool { return actors.mutations.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
				visible, err = store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.NoError(t, err)
				require.Equal(t, a2a.TaskStateCompleted, visible.Status.State)
				_, err = store.ClaimSessionQuiescence(t.Context())
				require.ErrorIs(t, err, database.ErrNotFound, "an uncertain suspension cannot be reassigned")
				pending, err := store.HasPendingSessionQuiescence(t.Context())
				require.NoError(t, err)
				require.False(t, pending, "uncertain claims must not keep recovery retries active")
			} else {
				require.Eventually(t, func() bool {
					_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), checkpoint, "alice", "checkpoint")
					return err == nil && snapshot.URI == "s3://snapshots/snapshot-1"
				}, 5*time.Second, 10*time.Millisecond)
				require.EqualValues(t, 1, actors.mutations.Load(), "database retries must not suspend the actor again")
				require.Equal(t, test.finishFailures+1, writes.attempts.Load())
			}
		})
	}
}

type quiescenceRetryStore struct {
	*lifecycleTestStore
	failures int32
	attempts atomic.Int32
}

func (s *quiescenceRetryStore) FinishSessionQuiescence(ctx context.Context, work *database.SessionQuiescence, snapshot *database.SessionTaskSnapshot) error {
	if s.attempts.Add(1) <= s.failures {
		return status.Error(codes.Unavailable, "database unavailable")
	}
	return s.Client.FinishSessionQuiescence(ctx, work, snapshot)
}
