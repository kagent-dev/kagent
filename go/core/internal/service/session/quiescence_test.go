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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric/noop"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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

// wakeQuiescence simulates taskstore's bounded, nonblocking settlement hint.
func (w *ActorWorkflow) wakeQuiescence() {
	select {
	case w.quiescenceWake <- struct{}{}:
	default:
	}
}

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
		go func() {
			done <- NewActorWorkflow(store, nil, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: 20 * time.Minute}, noop.NewMeterProvider()).Start(ctx)
		}()
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
		workflow := NewActorWorkflow(store, nil, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour}, noop.NewMeterProvider())
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
				go func() {
					done <- NewActorWorkflow(store, nil, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour}, noop.NewMeterProvider()).Start(ctx)
				}()
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
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Second}, noop.NewMeterProvider()).Create(t.Context(), session)
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
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour}, noop.NewMeterProvider())
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
			checkpoint := &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
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

func TestCanceledDispatchCleansUpOnlyItsIdleActor(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		state    ateapipb.ActorState
		missing  bool
		replaced bool
		resumes  int32
		suspends int32
	}{
		{name: "running actor without a turn", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, suspends: 1},
		{name: "interrupted resume without a turn", state: ateapipb.ActorState_ACTOR_STATE_RESUMING, resumes: 1, suspends: 1},
		{name: "interrupted suspension", state: ateapipb.ActorState_ACTOR_STATE_SUSPENDING, suspends: 1},
		{name: "already suspended actor", state: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "crashed actor", state: ateapipb.ActorState_ACTOR_STATE_CRASHED},
		{name: "missing actor", missing: true},
		{name: "replaced actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, replaced: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			writes := &dispatchCleanupTestStore{lifecycleTestStore: store}
			actors := &dispatchCleanupTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour, DispatchCleanupDelay: time.Millisecond}, noop.NewMeterProvider())
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			key := actorKey("team-a", "session-"+session.Id)
			actors.actors[key].Status.State = scenario.state
			if scenario.replaced {
				actors.actors[key].Metadata.Uid = "replacement"
			}
			if scenario.missing {
				delete(actors.actors, key)
			}
			startQuiescenceWorker(t, workflow)
			id := uuid.New()
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, id, ""))
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("canceled before admission"))
			message.ContextID = session.ContextId
			interactions := NewInteractionService(store, nil, NewService(store, serviceTestAuthorizer{}, workflow), false, false)
			notAccepted, err := interactions.RevokeSend(serviceTestContext("alice"), types.NamespacedName{Namespace: "team-a", Name: "assistant"}, message, id, true)
			require.NoError(t, err)
			require.True(t, notAccepted)
			require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 5*time.Second, time.Millisecond)
			require.Equal(t, scenario.resumes, actors.resumes.Load())
			require.Equal(t, scenario.suspends, actors.suspends.Load())
			if scenario.suspends != 0 {
				actor, err := actors.GetActor(t.Context(), "team-a", "session-"+session.Id)
				require.NoError(t, err)
				require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
			}
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), ""), "a new message can still be dispatched")
		})
	}
}

func TestDispatchCleanupDoesNotBlockNewSendDuringSuspension(t *testing.T) {
	store, session := lifecycleFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	actors := &dispatchCleanupTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, beforeSuspend: func(ctx context.Context) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	writes := &dispatchCleanupTestStore{lifecycleTestStore: store}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour, DispatchCleanupDelay: time.Millisecond}, noop.NewMeterProvider())
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	actors.actors[actorKey("team-a", "session-"+session.Id)].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	id := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, id, ""))
	revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, id, "canceled")
	require.NoError(t, err)
	startQuiescenceWorker(t, workflow)
	workflow.QueueDispatchCleanup(t.Context(), session.Id, revoked.CleanupID)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never reached suspension")
	}
	newID := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, newID, ""), "an issued suspension does not block admission")
	close(release)
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 5*time.Second, time.Millisecond)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("new input"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	hash := sha256.Sum256([]byte(task.ID))
	_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, newID.String())
	require.NoError(t, err, "the next message must start through normal admission")
}

func TestDispatchCleanupRetriesResumeAndSnapshotPersistence(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, resumeFailures: 2}
	writes := &dispatchCleanupTestStore{lifecycleTestStore: store, finishFailures: 2}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), ActorWorkflowConfig{RecoveryInterval: time.Hour}, noop.NewMeterProvider())
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	boundary := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, boundary.TaskId, boundary.Version))
	previous, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), previous, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/before-cleanup", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))
	actors.actors[actorKey("team-a", "session-"+session.Id)].Status.State = ateapipb.ActorState_ACTOR_STATE_RESUMING
	id := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, id, ""))
	revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, id, "canceled")
	require.NoError(t, err)
	request := &dispatchCleanup{sessionID: session.Id, cleanupID: revoked.CleanupID, deadline: time.Now().Add(dispatchCleanupBudget)}
	for range 2 {
		require.Error(t, workflow.attemptDispatchCleanup(t.Context(), request))
		require.Nil(t, request.work, "a runtime failure retries from current session state")
	}
	for range 2 {
		require.Error(t, workflow.attemptDispatchCleanup(t.Context(), request))
		require.NotNil(t, request.work, "a successful suspension keeps its outcome for database retries")
	}
	require.NoError(t, workflow.attemptDispatchCleanup(t.Context(), request))
	require.EqualValues(t, 3, actors.resumes.Load())
	require.EqualValues(t, 1, actors.suspends.Load(), "persistence retries must not repeat runtime calls")
	_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: boundary.TaskId}, "alice", uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, "s3://snapshots/snapshot-1", snapshot.URI, "cleanup must replace the prior boundary's snapshot reference")
}

func TestDispatchCleanupStopsWhenNewSendSupersedesResume(t *testing.T) {
	for _, duringResume := range []bool{false, true} {
		name := "Given failed cleanup, when a new message arrives during backoff, assert that cleanup stops"
		if duringResume {
			name = "Given a pending resume, when a new message arrives, assert that cleanup skips suspension"
		}
		t.Run(name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, resumeFailures: 1}
			workflow := NewActorWorkflow(store, actors, nil, ActorWorkflowConfig{RecoveryInterval: time.Hour}, noop.NewMeterProvider())
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			actors.actors[actorKey("team-a", "session-"+session.Id)].Status.State = ateapipb.ActorState_ACTOR_STATE_RESUMING
			id := uuid.New()
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, id, ""))
			revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, id, "canceled")
			require.NoError(t, err)
			request := &dispatchCleanup{sessionID: session.Id, cleanupID: revoked.CleanupID, deadline: time.Now().Add(dispatchCleanupBudget)}
			newID := uuid.New()
			if duringResume {
				actors.resumeFailures = 0
				actors.afterResume = func() { require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, newID, "")) }
			} else {
				require.Error(t, workflow.attemptDispatchCleanup(t.Context(), request))
				require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, newID, ""))
			}
			require.NoError(t, workflow.attemptDispatchCleanup(t.Context(), request))
			require.EqualValues(t, 1, actors.resumes.Load())
			require.Zero(t, actors.suspends.Load())
			current, err := store.RevokeSessionDispatch(t.Context(), session.Id, newID, "new input")
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, current.CleanupID)
		})
	}
}

type dispatchCleanupTestStore struct {
	*lifecycleTestStore
	finishFailures int32
	attempts       atomic.Int32
	finished       atomic.Int32
}

func (s *dispatchCleanupTestStore) FinishSessionDispatchCleanup(ctx context.Context, work *database.SessionDispatchCleanup, snapshot *database.SessionTaskSnapshot) error {
	if s.attempts.Add(1) <= s.finishFailures {
		return status.Error(codes.Unavailable, "database unavailable")
	}
	err := s.Client.FinishSessionDispatchCleanup(ctx, work, snapshot)
	if err == nil {
		s.finished.Add(1)
	}
	return err
}

type dispatchCleanupTestActors struct {
	*lifecycleTestActors
	resumes        atomic.Int32
	suspends       atomic.Int32
	resumeFailures int32
	beforeSuspend  func(context.Context)
	afterResume    func()
}

func (a *dispatchCleanupTestActors) ResumeActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if a.resumes.Add(1) <= a.resumeFailures {
		return nil, status.Error(codes.Aborted, "resume lease held by router")
	}
	actor, err := a.lifecycleTestActors.ResumeActor(ctx, atespace, name)
	if a.afterResume != nil {
		a.afterResume()
	}
	return actor, err
}

func (a *dispatchCleanupTestActors) SuspendActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.suspends.Add(1)
	if a.beforeSuspend != nil {
		a.beforeSuspend(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.lifecycleTestActors.SuspendActor(ctx, atespace, name)
}

func TestDispatchCleanupBackoffFreesWorkersAndStopsAtBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &backoffCleanupTestStore{calls: make(map[string][]time.Time)}
		const grace = 2 * time.Minute
		workflow := NewActorWorkflow(store, nil, nil, ActorWorkflowConfig{RecoveryInterval: time.Hour, DispatchCleanupDelay: grace}, nil)
		done := make(chan struct{})
		go func() { workflow.runDispatchCleanups(ctx); close(done) }()
		started := time.Now()
		for _, id := range []string{"busy-a", "busy-b", "busy-c", "superseded"} {
			workflow.QueueDispatchCleanup(ctx, id, uuid.New())
		}
		synctest.Wait()
		time.Sleep(grace - time.Millisecond)
		synctest.Wait()
		store.mu.Lock()
		earlyCalls := len(store.calls)
		store.mu.Unlock()
		require.Zero(t, earlyCalls, "no cleanup work may run during the grace period")
		time.Sleep(time.Millisecond)
		synctest.Wait()
		store.mu.Lock()
		witness := len(store.calls["superseded"])
		store.mu.Unlock()
		require.Equal(t, 1, witness, "backoff must leave workers free to handle other sessions")
		time.Sleep(dispatchCleanupBudget + dispatchCleanupMaxBackoff)
		synctest.Wait()
		store.mu.Lock()
		callsBySession := store.calls
		store.mu.Unlock()
		for _, id := range []string{"busy-a", "busy-b", "busy-c"} {
			calls := callsBySession[id]
			require.Greater(t, len(calls), 3)
			for i, call := range calls {
				require.True(t, call.Before(started.Add(grace+dispatchCleanupBudget)), "no database or runtime attempt may start after the budget")
				if i > 0 {
					require.LessOrEqual(t, call.Sub(calls[i-1]), dispatchCleanupMaxBackoff)
				}
			}
		}
		cancel()
		<-done
	})
}

func TestDisabledDispatchCleanupDoesNotStartWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given zero delay, even a revoked dispatch must not access the store or
		// runtime. Nil dependencies fail immediately if cleanup tries either.
		workflow := NewActorWorkflow(nil, nil, nil, ActorWorkflowConfig{}, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		workflow.QueueDispatchCleanup(ctx, uuid.NewString(), uuid.New())
		workflow.runDispatchCleanups(ctx)
		require.NoError(t, ctx.Err(), "disabled workers return without waiting for shutdown")
		require.Empty(t, workflow.dispatchCleanups, "disabled cleanup must not queue work")
		require.Zero(t, workflow.dispatchCleanupCount.Load())
	})
}

// backoffCleanupTestStore simulates a temporary checkpoint blocker. No runtime
// calls are possible; a superseded request is retired on its first attempt.
type backoffCleanupTestStore struct {
	workflowStore
	mu    sync.Mutex
	calls map[string][]time.Time
}

func (s *backoffCleanupTestStore) GetSessionDispatchCleanup(_ context.Context, sessionID string, _ uuid.UUID) (*database.SessionDispatchCleanup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[sessionID] = append(s.calls[sessionID], time.Now())
	if sessionID == "superseded" {
		return nil, database.ErrNotFound
	}
	return nil, database.ErrDispatchBusy
}
