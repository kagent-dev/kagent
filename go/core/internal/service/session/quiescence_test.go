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
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
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

func (s *idleQuiescenceStore) ClaimSessionQuiescence(context.Context, time.Duration, time.Duration, []string) (*database.SessionQuiescence, error) {
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

// testClaimLease outlives every test that does not wait for a claim to expire.
const testClaimLease = time.Hour

func TestIdleLifecycleDoesNotOwnTaskPublication(t *testing.T) {
	for _, test := range []struct {
		name           string
		mutationFails  bool
		finishFailures int32
	}{
		{name: "snapshot succeeds"},
		{name: "lost suspend response settles from the Actor", mutationFails: true},
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
			var enter sync.Once
			actors := &retryTestActors{lifecycleTestActors: base, beforeRead: func(ctx context.Context) {
				enter.Do(func() { close(entered) })
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

			// A suspend whose response was lost is recorded from the Actor's
			// snapshot, never by suspending it again.
			require.Eventually(t, func() bool {
				_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), checkpoint, "alice", "checkpoint")
				return err == nil && snapshot.URI == "s3://snapshots/snapshot-1"
			}, 5*time.Second, 10*time.Millisecond)
			require.EqualValues(t, 1, actors.mutations.Load(), "database retries must not suspend the actor again")
			require.Equal(t, test.finishFailures+1, writes.attempts.Load())
			_, err = store.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}
}

// A Pause or Quiesce that fails without an answer leaves the claim held while
// the Actor is still in transition, and the Actor's state then settles it: a
// boundary that landed is recorded, a running Actor gets the request once more,
// and any other state, or a retry that fails too, releases the claim without a
// snapshot and records why on the session. Either way the next turn is admitted.
func TestFailedBoundarySettlesFromTheActor(t *testing.T) {
	suspend := func(a *lifecycleTestActors, space, name string) {
		_, _ = a.SuspendActor(context.Background(), space, name)
	}
	pause := func(a *lifecycleTestActors, space, name string) {
		_, _ = a.PauseActor(context.Background(), space, name)
	}
	become := func(state ateapipb.ActorState) func(*lifecycleTestActors, string, string) {
		return func(a *lifecycleTestActors, space, name string) { a.setState(space, name, state) }
	}
	gone := func(a *lifecycleTestActors, space, name string) {
		_ = a.DeleteActor(context.Background(), space, name, true)
	}
	running := become(ateapipb.ActorState_ACTOR_STATE_RUNNING)
	retryFails := status.Error(codes.DeadlineExceeded, "sandbox teardown outlived the retry")
	for _, test := range []struct {
		name         string
		waiting      bool
		settle       func(*lifecycleTestActors, string, string)
		retryErr     error
		retryCrashes bool
		snapshot     bool
		requests     int32
		failure      string
		failedMsg    string
	}{
		{name: "suspend lands", settle: suspend, snapshot: true, requests: 1},
		{name: "suspend did not happen and its retry lands", settle: running, snapshot: true, requests: 2},
		{name: "suspend did not happen and its retry fails", settle: running, retryErr: retryFails, requests: 2, failure: "SuspendFailed", failedMsg: "sandbox teardown outlived the retry"},
		{name: "suspend did not happen and the runtime crashed during its retry", settle: running, retryErr: retryFails, retryCrashes: true, requests: 2, failure: "RuntimeUnavailable", failedMsg: "ACTOR_STATE_CRASHED"},
		{name: "runtime crashed", settle: become(ateapipb.ActorState_ACTOR_STATE_CRASHED), requests: 1, failure: "RuntimeUnavailable", failedMsg: "ACTOR_STATE_CRASHED"},
		{name: "actor gone", settle: gone, requests: 1, failure: "RuntimeUnavailable", failedMsg: "not found"},
		{name: "pause lands", waiting: true, settle: pause, requests: 1},
		{name: "pause did not happen and its retry lands", waiting: true, settle: running, requests: 2},
		{name: "pause did not happen and its retry fails", waiting: true, settle: running, retryErr: retryFails, requests: 2, failure: "PauseFailed", failedMsg: "sandbox teardown outlived the retry"},
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
			pending := ateapipb.ActorState_ACTOR_STATE_SUSPENDING
			if test.waiting {
				task.Status.State = a2a.TaskStateInputRequired
				pending = ateapipb.ActorState_ACTOR_STATE_PAUSING
			}
			hash := sha256.Sum256([]byte("turn"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
			require.NoError(t, err)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))

			actors := &unsettledActors{lifecycleTestActors: base, pending: pending, settle: test.settle, retryErr: test.retryErr, retryCrashes: test.retryCrashes, release: make(chan struct{}), reads: make(chan struct{}, 1)}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			workflow.settleDelay = time.Millisecond
			work, err := store.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
			require.NoError(t, err)
			workflow.quiesceIdleSession(t.Context(), work)
			for range 2 {
				select {
				case <-actors.reads:
				case <-time.After(5 * time.Second):
					t.Fatal("the unsettled Actor was not read again")
				}
			}
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "held"), database.ErrDispatchBusy, "an Actor in transition keeps the claim")

			close(actors.release)
			workflow.settling.Wait()
			require.Equal(t, test.requests, actors.mutations.Load(), "only a running Actor gets the request once more")
			_, err = store.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
			require.ErrorIs(t, err, database.ErrNotFound, "the settled boundary is not claimed again")
			settled, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			if test.failure == "" {
				require.Nil(t, settled.GetLastQuiescenceFailure(), "a settled boundary records no failure")
			} else {
				failure := settled.GetLastQuiescenceFailure()
				require.Equal(t, string(task.ID), failure.GetTaskId())
				require.Equal(t, test.failure, failure.GetReason())
				require.Contains(t, failure.GetMessage(), test.failedMsg)
				require.NotNil(t, failure.GetFailedAt())
			}
			if test.snapshot {
				checkpoint := &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
				_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), checkpoint, "alice", "checkpoint")
				require.NoError(t, err)
				require.Equal(t, "s3://snapshots/snapshot-1", snapshot.URI)
				return
			}
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), "the settled claim admits the next turn")
		})
	}
}

// A worker that stops while its claim is unsettled, during the runtime request
// or while it reads the Actor again, leaves the claim to another worker once
// the lease has run out. The other worker settles it from the Actor's state,
// repeating the request only on a running Actor; a live holder keeps its claim
// past the lease.
func TestStoppedHolderBoundaryIsSettledByAnotherWorker(t *testing.T) {
	suspend := func(a *lifecycleTestActors, space, name string) {
		_, _ = a.SuspendActor(context.Background(), space, name)
	}
	pause := func(a *lifecycleTestActors, space, name string) {
		_, _ = a.PauseActor(context.Background(), space, name)
	}
	for _, test := range []struct {
		name     string
		waiting  bool
		hang     bool
		settle   func(*lifecycleTestActors, string, string)
		requests int32
	}{
		{name: "stopped while settling a suspend", settle: suspend, requests: 1},
		{name: "stopped while settling a pause", waiting: true, settle: pause, requests: 1},
		{name: "stopped during the suspend request", hang: true, settle: suspend, requests: 1},
		{name: "stopped while settling an unfinished suspend", settle: func(a *lifecycleTestActors, space, name string) {
			a.setState(space, name, ateapipb.ActorState_ACTOR_STATE_RUNNING)
		}, requests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			const lease = 300 * time.Millisecond
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			message.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			task.Status.State = a2a.TaskStateCompleted
			pending := ateapipb.ActorState_ACTOR_STATE_SUSPENDING
			if test.waiting {
				task.Status.State = a2a.TaskStateInputRequired
				pending = ateapipb.ActorState_ACTOR_STATE_PAUSING
			}
			hash := sha256.Sum256([]byte("turn"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
			require.NoError(t, err)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))

			actors := &unsettledActors{lifecycleTestActors: base, pending: pending, settle: test.settle, release: make(chan struct{}), reads: make(chan struct{}, 1), hang: test.hang}
			stopped, stop := context.WithCancel(t.Context())
			first := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			first.settleDelay, first.claimLease = time.Millisecond, lease
			work, err := store.ClaimSessionQuiescence(t.Context(), lease, 0, nil)
			require.NoError(t, err)
			issued := make(chan struct{})
			go func() { defer close(issued); first.quiesceIdleSession(stopped, work) }()
			require.Eventually(t, func() bool { return actors.mutations.Load() == 1 }, 5*time.Second, time.Millisecond)

			// The live holder renews its claim for longer than one lease.
			time.Sleep(3 * lease)
			_, err = store.ClaimSessionQuiescence(t.Context(), lease, 0, nil)
			require.ErrorIs(t, err, database.ErrNotFound, "a renewed claim is not taken over")
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "held"), database.ErrDispatchBusy)

			stop()
			<-issued
			first.settling.Wait()
			close(actors.release)

			ctx, cancel := context.WithCancel(t.Context())
			second := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			second.settleDelay, second.claimLease = time.Millisecond, lease
			done := make(chan error, 1)
			go func() { done <- second.Start(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
			require.Eventually(t, func() bool {
				return store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next") == nil
			}, 10*time.Second, 20*time.Millisecond, "the boundary is settled by the second worker and admits the next turn")
			require.Equal(t, test.requests, actors.mutations.Load(), "the second worker repeats the runtime request only on a running Actor")
		})
	}
}

// A holder whose Pause is in flight when it freezes past its lease is taken
// over: the successor finds the Actor running and pauses it with a newer
// fencing token, and the late Pause is refused, so the boundary is settled once
// and the next turn is admitted. A live holder renewing its lease keeps its
// claim and its slow Pause lands.
func TestLatePauseOfATakenOverHolderIsRefused(t *testing.T) {
	for _, test := range []struct {
		name   string
		frozen bool
	}{
		{name: "frozen holder", frozen: true},
		{name: "live holder"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const lease = 300 * time.Millisecond
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			atespace, name := store.revision.ActorTemplateAtespace, substrate.ActorName(session.Id)
			base.setState(atespace, name, ateapipb.ActorState_ACTOR_STATE_RUNNING)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			message.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			task.Status.State = a2a.TaskStateInputRequired
			hash := sha256.Sum256([]byte("turn"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
			require.NoError(t, err)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))

			actors := &delayedPauseActors{lifecycleTestActors: base, issued: make(chan struct{}), land: make(chan struct{}), landed: make(chan error, 1)}
			var holderStore workflowStore = store
			if test.frozen {
				holderStore = &frozenRenewalStore{lifecycleTestStore: store}
			}
			first := NewActorWorkflow(holderStore, actors, make(chan struct{}, 1), time.Second)
			first.claimLease = lease
			work, err := store.ClaimSessionQuiescence(t.Context(), lease, 0, nil)
			require.NoError(t, err)
			holding := make(chan struct{})
			go func() { defer close(holding); first.quiesceIdleSession(t.Context(), work) }()
			select {
			case <-actors.issued:
			case <-time.After(5 * time.Second):
				t.Fatal("the holder did not issue its Pause")
			}

			ctx, cancel := context.WithCancel(t.Context())
			second := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			second.settleDelay, second.claimLease = time.Millisecond, lease
			done := make(chan error, 1)
			go func() { done <- second.Start(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

			if !test.frozen {
				time.Sleep(3 * lease)
				require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "held"), database.ErrDispatchBusy, "a renewed claim is not taken over")
				close(actors.land)
				require.NoError(t, <-actors.landed)
				<-holding
				actor, err := base.GetActor(t.Context(), atespace, name)
				require.NoError(t, err)
				require.Equal(t, ateapipb.ActorState_ACTOR_STATE_PAUSED, actor.GetStatus().GetState())
				require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), "the finished pause admits the next turn")
				return
			}

			require.Eventually(t, func() bool {
				return store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next") == nil
			}, 10*time.Second, 20*time.Millisecond, "the successor settles the boundary and admits the next turn")
			close(actors.land)
			require.Equal(t, codes.FailedPrecondition, status.Code(<-actors.landed), "the late Pause of the taken-over holder is refused")
			<-holding
			actor, err := base.GetActor(t.Context(), atespace, name)
			require.NoError(t, err)
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_PAUSED, actor.GetStatus().GetState(), "the successor's own Pause settled the boundary")
		})
	}
}

// delayedPauseActors holds the first Pause until land is closed, as a request
// in flight while its sender freezes, and reports what Substrate made of it.
// Later Pauses pass straight through.
type delayedPauseActors struct {
	*lifecycleTestActors
	issued, land chan struct{}
	landed       chan error
	first        atomic.Bool
}

func (a *delayedPauseActors) PauseActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if !a.first.CompareAndSwap(false, true) {
		return a.lifecycleTestActors.PauseActor(ctx, space, name)
	}
	close(a.issued)
	<-a.land
	actor, err := a.lifecycleTestActors.PauseActor(ctx, space, name)
	a.landed <- err
	return actor, err
}

// frozenRenewalStore stands for a holder frozen past its lease: its renewals
// never reach the database.
type frozenRenewalStore struct {
	*lifecycleTestStore
}

func (*frozenRenewalStore) RenewSessionQuiescence(context.Context, *database.SessionQuiescence, time.Duration) error {
	return nil
}

// unsettledActors fails the first boundary request after Substrate took it: the
// Actor reports pending until release is closed, and then what settle made of
// it. With hang, the request does not return until its caller stops. A later
// request lands, or fails with retryErr leaving the Actor as it is, or crashed
// with retryCrashes.
type unsettledActors struct {
	*lifecycleTestActors
	hang         bool
	retryErr     error
	retryCrashes bool
	pending      ateapipb.ActorState
	settle       func(*lifecycleTestActors, string, string)
	release      chan struct{}
	reads        chan struct{}
	failed       atomic.Bool
	settled      sync.Once
	mutations    atomic.Int32
}

func (a *unsettledActors) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if a.failed.Load() {
		select {
		case <-a.release:
			a.settled.Do(func() { a.settle(a.lifecycleTestActors, space, name) })
		default:
			select {
			case a.reads <- struct{}{}:
			default:
			}
		}
	}
	return a.lifecycleTestActors.GetActor(ctx, space, name)
}

func (a *unsettledActors) SuspendActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if a.mutations.Add(1) > 1 {
		if a.retryCrashes {
			a.setState(space, name, ateapipb.ActorState_ACTOR_STATE_CRASHED)
		}
		if a.retryErr != nil {
			return nil, a.retryErr
		}
		return a.lifecycleTestActors.SuspendActor(ctx, space, name)
	}
	return nil, a.fail(ctx, space, name)
}

func (a *unsettledActors) PauseActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if a.mutations.Add(1) > 1 {
		if a.retryErr != nil {
			return nil, a.retryErr
		}
		return a.lifecycleTestActors.PauseActor(ctx, space, name)
	}
	return nil, a.fail(ctx, space, name)
}

func (a *unsettledActors) fail(ctx context.Context, space, name string) error {
	a.setState(space, name, a.pending)
	a.failed.Store(true)
	if a.hang {
		<-ctx.Done()
	}
	return status.Error(codes.DeadlineExceeded, "sandbox teardown outlived the request")
}

func (a *lifecycleTestActors) setState(space, name string, state ateapipb.ActorState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actors[actorKey(space, name)].Status.State = state
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
