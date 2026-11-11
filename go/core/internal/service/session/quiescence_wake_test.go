package session

import (
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type observedQuiescenceStore struct {
	*lifecycleTestStore
	idle     atomic.Int32
	finished atomic.Int32
}

var _ workflowStore = (*observedQuiescenceStore)(nil)

func (s *observedQuiescenceStore) HasPendingSessionQuiescence(ctx context.Context) (bool, error) {
	pending, err := s.Client.HasPendingSessionQuiescence(ctx)
	s.idle.Add(1)
	return pending, err
}

func (s *observedQuiescenceStore) FinishSessionQuiescence(ctx context.Context, work *database.SessionQuiescence, snapshot *database.SessionTaskSnapshot) error {
	err := s.Client.FinishSessionQuiescence(ctx, work, snapshot)
	if err == nil {
		s.finished.Add(1)
	}
	return err
}

type gatedQuiescenceActors struct {
	*lifecycleTestActors
	release <-chan struct{}
	entered atomic.Int32
	mu      sync.Mutex
	calls   map[string]int
}

var _ actorClient = (*gatedQuiescenceActors)(nil)

func (a *gatedQuiescenceActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	a.calls[name]++
	a.mu.Unlock()
	a.entered.Add(1)
	select {
	case <-a.release:
		return a.lifecycleTestActors.GetActor(ctx, atespace, name)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func settlementContext(t *testing.T, session *apiv1.Session) context.Context {
	t.Helper()
	headers := http.Header{}
	i := jwttest.New(t)
	v, err := substrate.NewActorJWTVerifier(t.Context(), i.URL, i.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	headers.Set("authorization", "Bearer "+i.Token(t, i.Claims("team-a", substrate.ActorName(session.Id), "actor-uid")))
	identity, err := runtimeauth.NewAuthenticator(v).Authenticate(t.Context(), headers, nil)
	require.NoError(t, err)
	return auth.AuthSessionTo(t.Context(), identity)
}

func stageQuiescenceTask(t *testing.T, store *database.Client, session *apiv1.Session, state a2a.TaskState) *runtimev1.TaskStoreServiceSettleTaskRequest {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = state
	hash := sha256.Sum256([]byte(task.ID))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	return &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: session.Id, TaskId: string(task.ID), Version: version}
}

func startQuiescenceWorker(t *testing.T, workflow *ActorWorkflow) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- workflow.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
}

// Two API replicas share only PostgreSQL. Settlements arrive while all eight
// workers are blocked in runtime I/O; each must drain its claims after release.
func TestQuiescenceDrainsConcurrentSettlementsAcrossReplicas(t *testing.T) {
	store, first := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	creator := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider())
	var sessions []*apiv1.Session
	var requests []*runtimev1.TaskStoreServiceSettleTaskRequest
	states := []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired}
	for i := range 36 {
		session := first
		if i != 0 {
			var err error
			session, _, err = store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: first.GetAgent()}, uuid.NewString())
			require.NoError(t, err)
		}
		session, err := creator.Create(t.Context(), session)
		require.NoError(t, err)
		sessions = append(sessions, session)
		requests = append(requests, stageQuiescenceTask(t, store.Client, session, states[i%len(states)]))
	}
	release := make(chan struct{})
	actors := []*gatedQuiescenceActors{
		{lifecycleTestActors: base, release: release, calls: map[string]int{}},
		{lifecycleTestActors: base, release: release, calls: map[string]int{}},
	}
	writes := &observedQuiescenceStore{lifecycleTestStore: store}
	var workflows []*ActorWorkflow
	var services []*taskstore.Service
	for _, runtime := range actors {
		wake := make(chan struct{}, 1)
		workflow := NewActorWorkflow(writes, runtime, wake, time.Hour, noop.NewMeterProvider())
		startQuiescenceWorker(t, workflow)
		workflows = append(workflows, workflow)
		services = append(services, taskstore.NewService(store, wake, nil))
	}
	require.Eventually(t, func() bool { return writes.idle.Load() >= 8 }, 5*time.Second, 10*time.Millisecond)
	// Failed settlement leaves the task unpublished and no eligible work.
	invalid := &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: requests[0].SessionId, TaskId: requests[0].TaskId, Version: requests[0].Version + 1}
	_, err := services[0].SettleTask(settlementContext(t, sessions[0]), invalid)
	require.Error(t, err)
	pending, err := store.HasPendingSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.False(t, pending)

	// Fill one replica before waking the other. PostgreSQL does not promise
	// fair assignment when both replicas compete for the same small burst.
	// Deliver one coalesced hint per burst to exercise the workers' handoff.
	coalesced := taskstore.NewService(store, nil, nil)
	for replica := range 2 {
		for i := replica * 4; i < (replica+1)*4; i++ {
			_, err := coalesced.SettleTask(settlementContext(t, sessions[i]), requests[i])
			require.NoError(t, err)
		}
		workflows[replica].wakeQuiescence()
		require.Eventually(t, func() bool { return actors[replica].entered.Load() == 4 }, 5*time.Second, 10*time.Millisecond)
	}
	var settlements sync.WaitGroup
	errors := make(chan error, len(requests))
	for i := 8; i < len(requests); i++ {
		ctx := settlementContext(t, sessions[i])
		settlements.Go(func() {
			_, err := services[i%2].SettleTask(ctx, requests[i])
			errors <- err
		})
	}
	settlements.Wait() // Hints must not wait for the blocked runtime calls.
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.EqualValues(t, 4, actors[0].entered.Load())
	require.EqualValues(t, 4, actors[1].entered.Load())
	close(release)
	require.Eventually(t, func() bool { return writes.finished.Load() == int32(len(requests)) }, 5*time.Second, 10*time.Millisecond)
	for i, session := range sessions {
		name := substrate.ActorName(session.Id)
		calls := 0
		for _, runtime := range actors {
			runtime.mu.Lock()
			calls += runtime.calls[name]
			runtime.mu.Unlock()
		}
		require.Equal(t, 1, calls, "each durable claim must run exactly once across replicas")
		actor, err := base.GetActor(t.Context(), "team-a", name)
		require.NoError(t, err)
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
		require.Equal(t, ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, substrate.LatestActorSnapshot(actor).GetFidelity())
		require.NotEmpty(t, substrate.SnapshotURI(substrate.LatestActorSnapshot(actor)))
		if states[i%len(states)].Terminal() {
			_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: requests[i].TaskId}, "alice", uuid.NewString())
			require.NoError(t, err)
			require.Equal(t, "s3://snapshots/snapshot-1", snapshot.URI)
		}
	}
}

func TestQuiescenceRecoversMissedSignal(t *testing.T) {
	_, provider, reader, _ := captureWorkTelemetry(t)
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour, provider).Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateInputRequired)
	writes := &observedQuiescenceStore{lifecycleTestStore: store}
	startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), 100*time.Millisecond, provider))
	require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, time.Millisecond)
	// This simulates a commit on a replica that dies before sending its hint.
	_, err = taskstore.NewService(store, nil, nil).SettleTask(settlementContext(t, session), request)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, wantWorkClaims("recovery", 1), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
}

func TestQuiescenceRetriesTemporaryBlockers(t *testing.T) {
	for _, blocker := range []string{"dispatch", "lifecycle"} {
		t.Run(blocker, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider()).Create(t.Context(), session)
			require.NoError(t, err)
			_, err = actors.ResumeActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
			require.NoError(t, err)
			request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateInputRequired)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
			var unblock func()
			if blocker == "dispatch" {
				dispatchID := uuid.New()
				require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, ""))
				unblock = func() {
					revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, dispatchID, "unsaved-message")
					require.NoError(t, err)
					require.True(t, revoked)
				}
			} else {
				operation, err := store.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, false)
				require.NoError(t, err)
				unblock = func() {
					_, err := store.FinishSessionOperation(t.Context(), session.Id, operation.ID, uuid.Nil, "", "", "preparation failed before runtime I/O")
					require.NoError(t, err)
				}
			}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider()))
			require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, 10*time.Millisecond)
			require.Zero(t, writes.finished.Load()+writes.finishedCleanup.Load())
			pending, err := store.HasPendingSessionQuiescence(t.Context())
			require.NoError(t, err)
			require.True(t, pending, "an empty claim must distinguish blocked work from idle")
			unblock() // No signal: retry must discover the cleared blocker promptly.
			require.Eventually(t, func() bool { return writes.finished.Load()+writes.finishedCleanup.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
		})
	}
}

func TestQuiescenceCancellationRetainsClaim(t *testing.T) {
	store, session := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider()).Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
	actors := &gatedQuiescenceActors{lifecycleTestActors: base, release: make(chan struct{}), calls: map[string]int{}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider()).Start(ctx)
	}()
	require.Eventually(t, func() bool { return actors.entered.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel runtime work")
	}
	_, err = store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound)
	pending, err := store.HasPendingSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.False(t, pending, "a retained claim must not be treated as retryable work")
}

func TestAbandonedDispatchWakesLifecycleWorker(t *testing.T) {
	// Given a running actor whose first delivery has not accepted a task.
	store, actors, session := dispatchCleanupFixture(t)
	wake := make(chan struct{}, 1)
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	startQuiescenceWorker(t, NewActorWorkflow(writes, actors, wake, time.Hour, noop.NewMeterProvider()))
	require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, time.Millisecond)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	dispatchID := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, message.ID))
	service := NewInteractionService(store, nil, NewService(store, serviceTestAuthorizer{}, nil), false, false, wake)

	// When gateway cleanup revokes the delivery, its commit wakes the worker.
	agent := types.NamespacedName{Namespace: session.Agent.Ref.Namespace, Name: session.Agent.Ref.Name}
	notAccepted, err := service.RevokeSend(serviceTestContext(session.Creator), agent, message, dispatchID)
	require.NoError(t, err)
	require.True(t, notAccepted)

	// Assert that suspension does not wait for the recovery interval, create a
	// synthetic task, or change the Session's logical availability.
	require.Eventually(t, func() bool { return writes.finishedCleanup.Load() == 1 }, 5*time.Second, time.Millisecond)
	actor, err := actors.GetActor(t.Context(), agent.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	stored, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, stored.State)
	tasks, total, err := store.ListSessionTasks(t.Context(), session.Id, "", "", nil, 10, nil)
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.Zero(t, total)
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchWaitsForRestore(t *testing.T) {
	t.Run("Given a suspended actor, when restore finishes, assert that cleanup suspends it", func(t *testing.T) {
		store, base, session := dispatchCleanupFixture(t)
		setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
		abandonDispatch(t, store, session)
		actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
		writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
		startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), 20*time.Millisecond, noop.NewMeterProvider()))
		require.Eventually(t, func() bool { return actors.readCalls.Load() > 0 }, 5*time.Second, time.Millisecond)
		require.Zero(t, actors.suspendCalls.Load())
		// Observing an unfinished restore must not leave an issued claim that
		// prevents a replacement delivery from acquiring the Session.
		dispatchID := uuid.New()
		require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "replacement"))
		_, err := base.ResumeActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
		require.NoError(t, err)
		_, err = store.RevokeSessionDispatch(t.Context(), session.Id, dispatchID, "replacement")
		require.NoError(t, err)
		require.Eventually(t, func() bool { return writes.finishedCleanup.Load() == 1 }, 5*time.Second, time.Millisecond)
		require.EqualValues(t, 1, actors.suspendCalls.Load())
	})
}

func TestAbandonedDispatchCompletesInterruptedResume(t *testing.T) {
	// Given a canceled delivery with an actor stuck RESUMING and no task.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}

	// When cleanup retries the interrupted resume, it must then suspend under
	// the same claim instead of waiting for another client to drive restoration.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(t.Context()))

	// Assert that no turn was invented and the next delivery can be accepted.
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.EqualValues(t, 1, actors.suspendCalls.Load())
	require.EqualValues(t, 1, writes.finishedCleanup.Load())
	actor, err := base.GetActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	stored, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, stored.State)
	tasks, total, err := store.ListSessionTasks(t.Context(), session.Id, "", "", nil, 10, nil)
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.Zero(t, total)
	dispatchID := uuid.New()
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next"))
	message.ContextID = session.ContextId
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, message.ID))
	task := a2a.NewSubmittedTask(message, message)
	hash := sha256.Sum256([]byte(task.ID))
	_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, dispatchID.String())
	require.NoError(t, err)
}

func TestAbandonedDispatchPreservesExistingSuspension(t *testing.T) {
	for _, state := range []ateapipb.ActorState{ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_SUSPENDING} {
		t.Run(state.String(), func(t *testing.T) {
			// Given a canceled delivery whose actor is already suspending or suspended.
			// That state alone cannot rule out a delayed ingress resume.
			store, base, session := dispatchCleanupFixture(t)
			work := abandonDispatch(t, store, session)
			setDispatchCleanupActorState(base, session, state)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())

			// When cleanup sees the concurrent suspension, assert that it parks
			// durably instead of keeping the actor on the active retry queue.
			require.NoError(t, workflow.quiesceAbandonedDispatch(t.Context()))
			require.Zero(t, actors.resumeCalls.Load())
			require.Zero(t, actors.suspendCalls.Load())
			require.Zero(t, writes.finishedCleanup.Load())
			require.EqualValues(t, 1, writes.cleanupClaims.Load())
			require.EqualValues(t, 1, writes.parkedCleanup.Load())
			_, err := store.ClaimSessionDispatchCleanup(t.Context(), "")
			require.ErrorIs(t, err, database.ErrNotFound)
			_, err = store.RevokeSessionDispatch(t.Context(), session.Id, work.DispatchID, "abandoned-message")
			require.NoError(t, err)
			replacement := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			queue := newQuiescenceQueue()
			t.Cleanup(queue.ShutDown)
			_, cleanup, err := replacement.quiesceNextSession(t.Context(), queue, queue.scans[0])
			require.ErrorIs(t, err, database.ErrNotFound)
			require.Nil(t, cleanup)
			require.EqualValues(t, 1, actors.readCalls.Load(), "restart and repeated cancellation must preserve the parked schedule")

			// When concurrent suspension finishes and the original ingress resumes,
			// assert that the preserved delivery is rediscovered and suspended.
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
			_, err = base.ResumeActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
			require.NoError(t, err)
			makeDispatchCleanupDue(t, store, work)
			pending, err := replacement.claimAbandonedDispatch(t.Context(), "")
			require.NoError(t, err)
			require.Equal(t, work.DispatchID, pending.work.DispatchID)
			require.NoError(t, replacement.finishDispatchCleanup(t.Context(), pending))
			require.Zero(t, actors.resumeCalls.Load())
			require.EqualValues(t, 1, actors.suspendCalls.Load())
			require.EqualValues(t, 2, writes.cleanupClaims.Load())
			require.EqualValues(t, 1, writes.finishedCleanup.Load())
			_, err = store.ClaimSessionDispatchCleanup(t.Context(), "")
			require.ErrorIs(t, err, database.ErrNotFound)
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
		})
	}
}

func TestAbandonedDispatchRetriesParkingWithoutActorCalls(t *testing.T) {
	// Given an earlier suspension and two failed writes of its parked schedule.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}, parkFailures: 2}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	cleanup, err := workflow.claimAbandonedDispatch(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	// When persistence is retried, assert that the known observation is reused.
	for range writes.parkFailures {
		done, delay, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
		require.NoError(t, err)
		require.False(t, done)
		require.Positive(t, delay)
	}
	done, delay, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
	require.NoError(t, err)
	require.True(t, done)
	require.Zero(t, delay)
	require.EqualValues(t, 1, actors.readCalls.Load())
	require.Zero(t, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	require.EqualValues(t, 1, writes.parkedCleanup.Load())
	require.True(t, cleanup.work.AvailableAt.After(time.Now().Add(50*time.Second)))
	_, err = store.ClaimSessionDispatchCleanup(t.Context(), "")
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestAbandonedDispatchRejectsShortClaim(t *testing.T) {
	// Given abandoned work and a caller with too little time for runtime work.
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	// When it tries to claim, assert that no ownership or Actor call is issued.
	cleanup, err := workflow.claimAbandonedDispatch(ctx, "")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, cleanup)
	require.Zero(t, writes.cleanupClaims.Load())
	require.Zero(t, actors.readCalls.Load())
	require.Zero(t, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	_, err = store.ClaimSessionDispatchCleanup(t.Context(), "")
	require.NoError(t, err, "the rejected claim must leave work immediately available")
}

func TestLifecycleJobCounters(t *testing.T) {
	// Given instrumentation and a canceled resume that fails twice before recovery.
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(_ context.Context, call int32) error {
		if call <= 2 {
			return status.Error(codes.Unavailable, "interrupted resume")
		}
		return nil
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	wake := make(chan struct{}, 1)
	workflow := NewActorWorkflow(writes, actors, wake, time.Minute, provider)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- workflow.Start(ctx) }()
	var stopped bool
	t.Cleanup(func() {
		cancel()
		if !stopped {
			require.NoError(t, <-done)
		}
	})

	// When recovery finishes and another canceled delivery sees the earlier
	// suspension, assert that completed recovery and parking are counted separately.
	require.Eventually(t, func() bool { return writes.finishedCleanup.Load() == 1 }, 5*time.Second, time.Millisecond)
	abandonDispatch(t, store, session)
	workflow.wakeQuiescence()
	require.Eventually(t, func() bool { return writes.parkedCleanup.Load() == 1 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	stopped = true
	var measurements metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &measurements))
	counts := map[string]int64{}
	for _, scope := range measurements.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != "kagent.runtime_lifecycle.job.events" {
				continue
			}
			for _, point := range measurement.Data.(metricdata.Sum[int64]).DataPoints {
				kind, ok := point.Attributes.Value("kagent.runtime_lifecycle.job.kind")
				require.True(t, ok)
				require.Equal(t, database.RuntimeLifecycleJobRecoverAbandonedDispatch, kind.AsString())
				phase, ok := point.Attributes.Value("kagent.runtime_lifecycle.job.phase")
				require.True(t, ok)
				event, ok := point.Attributes.Value("kagent.runtime_lifecycle.job.event")
				require.True(t, ok)
				require.Equal(t, 3, point.Attributes.Len(), "runtime IDs must not become metric labels")
				counts[event.AsString()+"/"+phase.AsString()] += point.Value
			}
		}
	}
	require.Equal(t, map[string]int64{
		lifecycleEventClaim + "/" + database.RuntimeLifecycleJobRecovering:    2,
		lifecycleEventRetry + "/" + database.RuntimeLifecycleJobRecovering:    2,
		lifecycleEventPark + "/" + database.RuntimeLifecycleJobRecovering:     1,
		lifecycleEventSuspend + "/" + database.RuntimeLifecycleJobSuspending:  1,
		lifecycleEventComplete + "/" + database.RuntimeLifecycleJobSuspending: 1,
	}, counts)
}

func TestAbandonedDispatchRejectsChangedResumeIdentity(t *testing.T) {
	// Given a cleanup whose resume response belongs to a different actor UID.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	const replacementActorUID = "replacement-actor-uid"
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumedActorUID: replacementActorUID}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}

	// When resume returns that identity, assert that cleanup stops without
	// suspending the replacement or blocking a new message.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(t.Context()))
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	require.Zero(t, writes.finishedCleanup.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchRetriesInterruptedResume(t *testing.T) {
	for _, code := range []codes.Code{codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Internal, codes.ResourceExhausted, codes.FailedPrecondition} {
		t.Run("Given a resume returning "+code.String()+", when cleanup retries, assert that the actor suspends under one claim", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			abandonDispatch(t, store, session)
			var firstDeadline time.Time
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(ctx context.Context, call int32) error {
				require.NoError(t, ctx.Err())
				deadline, bounded := ctx.Deadline()
				require.True(t, bounded, "each runtime attempt must be bounded")
				if call == 1 {
					firstDeadline = deadline
					return status.Error(code, "temporary resume failure")
				}
				require.True(t, deadline.After(firstDeadline), "a retry needs a fresh attempt deadline")
				return nil
			}}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stop := time.AfterFunc(5*time.Second, cancel)
			defer stop.Stop()
			require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))

			require.EqualValues(t, 2, actors.resumeCalls.Load())
			require.EqualValues(t, 1, actors.suspendCalls.Load())
			require.EqualValues(t, 1, writes.cleanupClaims.Load())
			require.EqualValues(t, 1, writes.finishedCleanup.Load())
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
		})
	}
}

func TestAbandonedDispatchSuspendRetryDoesNotResumeAgain(t *testing.T) {
	for _, code := range []codes.Code{codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Internal, codes.ResourceExhausted, codes.FailedPrecondition} {
		t.Run("Given a successful resume and suspend returning "+code.String()+", when cleanup retries, assert that it only retries suspension", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			abandonDispatch(t, store, session)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base, suspendAttempt: func(_ context.Context, call int32) error {
				if call == 1 {
					setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_SUSPENDING)
					return status.Error(code, "temporary suspension failure")
				}
				return nil
			}}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))

			require.EqualValues(t, 1, actors.resumeCalls.Load())
			require.EqualValues(t, 2, actors.suspendCalls.Load())
			require.EqualValues(t, 1, writes.cleanupClaims.Load())
			require.EqualValues(t, 1, writes.finishedCleanup.Load())
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
		})
	}
}

func TestAbandonedDispatchRetriesLostSuspensionResponse(t *testing.T) {
	// Given an interrupted resume whose cleanup suspends the actor but loses
	// the response, leaving a valid snapshot behind an uncertain call outcome.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, suspendAttempt: func(ctx context.Context, call int32) error {
		if call == 1 {
			_, err := base.SuspendActor(ctx, session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
			require.NoError(t, err)
			return status.Error(codes.Unavailable, "suspension response lost")
		}
		return nil
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When cleanup retries against SUSPENDED, assert that the same owner
	// records completion and admits the next message without resuming again.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.EqualValues(t, 2, actors.suspendCalls.Load())
	require.EqualValues(t, 1, writes.cleanupClaims.Load())
	require.EqualValues(t, 1, writes.finishedCleanup.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchRetriesDoNotStarveSettledTasks(t *testing.T) {
	// Given four interrupted restores that fail indefinitely, filling every
	// lifecycle worker, and a later completed turn on another Session.
	const failedRestores = 4
	const abandonedMessage = "abandoned-message"
	store, first := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	creator := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider())
	var sessions []*apiv1.Session
	for i := range failedRestores {
		session := first
		if i != 0 {
			var err error
			session, _, err = store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: first.GetAgent()}, uuid.NewString())
			require.NoError(t, err)
		}
		session, err := creator.Create(t.Context(), session)
		require.NoError(t, err)
		setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
		dispatchID := uuid.New()
		require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, abandonedMessage))
		revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, dispatchID, abandonedMessage)
		require.NoError(t, err)
		require.True(t, revoked)
		sessions = append(sessions, session)
	}
	var failResume atomic.Bool
	failResume.Store(true)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(context.Context, int32) error {
		if failResume.Load() {
			return status.Error(codes.Unavailable, "restore is temporarily unavailable")
		}
		return nil
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider())
	startQuiescenceWorker(t, workflow)
	require.Eventually(t, func() bool { return writes.cleanupClaims.Load() == failedRestores }, 5*time.Second, time.Millisecond)

	healthy, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: first.GetAgent()}, uuid.NewString())
	require.NoError(t, err)
	healthy, err = creator.Create(t.Context(), healthy)
	require.NoError(t, err)
	_, err = base.ResumeActor(t.Context(), healthy.Agent.Ref.Namespace, substrate.ActorName(healthy.Id))
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, healthy, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), healthy.Id, request.TaskId, request.Version))

	// When the completed turn wakes the workers, assert that its suspension
	// finishes while each failing restore retains its worker claim.
	workflow.wakeQuiescence()
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 3*time.Second, time.Millisecond,
		"retrying restores must yield workers to another Session's completed turn")
	require.Zero(t, writes.finishedCleanup.Load())
	require.EqualValues(t, failedRestores, writes.cleanupClaims.Load())
	actor, err := base.GetActor(t.Context(), healthy.Agent.Ref.Namespace, substrate.ActorName(healthy.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())

	// When restoration becomes available, assert that the retained owners
	// finish cleanup without releasing and reacquiring the claims between tries.
	failResume.Store(false)
	workflow.wakeQuiescence()
	require.Eventually(t, func() bool { return writes.finishedCleanup.Load() == failedRestores }, 10*time.Second, time.Millisecond)
	require.EqualValues(t, failedRestores, writes.cleanupClaims.Load())
	for _, session := range sessions {
		actor, err := base.GetActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
		require.NoError(t, err)
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
		require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
	}
}

func TestAbandonedDispatchRejectsReplacementBeforeRetry(t *testing.T) {
	// Given a failed resume followed by an externally replaced actor.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	const replacementActorUID = "replacement-actor-uid"
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(context.Context, int32) error {
		base.mu.Lock()
		base.actors[actorKey(session.Agent.Ref.Namespace, substrate.ActorName(session.Id))].Metadata.Uid = replacementActorUID
		base.mu.Unlock()
		return status.Error(codes.Unavailable, "resume response lost")
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When cleanup rereads before retrying, assert that it rejects the changed
	// identity without mutating the replacement or blocking a new message.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	require.Zero(t, writes.finishedCleanup.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchCrashRecoverySupersedesCleanup(t *testing.T) {
	// Given a resume whose server confirms a crash, but has not returned yet.
	store, base, session := dispatchCleanupFixture(t)
	_, err := base.SuspendActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	entered, release := make(chan struct{}), make(chan struct{})
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(ctx context.Context, _ int32) error {
		setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_CRASHED)
		close(entered)
		select {
		case <-release:
			return status.Error(codes.FailedPrecondition, "actor crashed during resume")
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- workflow.quiesceAbandonedDispatch(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("cleanup did not start resume")
	}

	// When recovery races an issued call, assert that it supersedes cleanup.
	// The old call may finish, but the worker must not issue another mutation.
	recovered, err := workflow.Revert(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, recovered.State)
	close(release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("cleanup did not finish after the conclusive crash")
	}
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	require.Zero(t, writes.finishedCleanup.Load(), "a crash is not a successful cleanup snapshot")
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchUncertainCrashAllowsForceDeletion(t *testing.T) {
	// Given an interrupted restore whose Internal reply leaves a crashed
	// actor and an unfinished cleanup owner.
	store, base, session := dispatchCleanupFixture(t)
	_, err := base.SuspendActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	deleteEntered, releaseDelete := make(chan struct{}), make(chan struct{})
	actors := &gatedDispatchDeletionActors{
		dispatchCleanupTestActors: &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(context.Context, int32) error {
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_CRASHED)
			return status.Error(codes.Internal, "restore failed after the actor crashed")
		}},
		deleteEntered: deleteEntered,
		releaseDelete: releaseDelete,
	}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	cleanup, err := workflow.claimAbandonedDispatch(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	done, delay, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
	require.NoError(t, err)
	require.False(t, done, "an Internal reply must retain ownership for retry")
	require.Positive(t, delay, "failed restore must wait before retrying")

	// When force deletion takes ownership, gate its backend call so a queued
	// cleanup retry observes DELETING before the actor has been removed.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	deleted := make(chan *apiv1.Session, 1)
	deleteErrors := make(chan error, 1)
	deleteFinished := make(chan struct{})
	go func() {
		defer close(deleteFinished)
		result, err := workflow.Delete(ctx, session, true)
		deleted <- result
		deleteErrors <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-deleteFinished
	})
	select {
	case <-deleteEntered:
	case err := <-deleteErrors:
		t.Fatalf("force deletion did not reach the actor: %v", err)
	case <-ctx.Done():
		t.Fatal("force deletion did not start")
	}
	current, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, current.State)
	reads := actors.readCalls.Load()
	done, _, err = workflow.retryDispatchCleanup(t.Context(), cleanup)
	require.NoError(t, err)
	require.True(t, done, "queued cleanup must stop after deletion takes ownership")
	require.Equal(t, reads, actors.readCalls.Load(), "cleanup must not contact the runtime after observing deletion")
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())

	// Assert that force deletion finishes while the obsolete cleanup owner
	// neither releases admission nor records a successful suspension.
	close(releaseDelete)
	select {
	case err := <-deleteErrors:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("force deletion did not finish")
	}
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, (<-deleted).State)
	_, err = base.GetActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Zero(t, writes.finishedCleanup.Load())
}

func TestAbandonedDispatchStopsAfterExternalDeletionStarts(t *testing.T) {
	// Given an interrupted resume whose next observation is an externally
	// deleting actor, cleanup no longer has a supported transition to retry.
	store, base, session := dispatchCleanupFixture(t)
	setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(context.Context, int32) error {
		setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_DELETING)
		return status.Error(codes.Unavailable, "resume response lost")
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When cleanup rereads that state, assert that it stops without issuing
	// another mutation or blocking a new message.
	require.NoError(t, NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))
	require.NoError(t, ctx.Err(), "unsupported actor state must stop retries before the context expires")
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchCrashStopsCleanup(t *testing.T) {
	for _, phase := range []string{"resume", "suspend after a successful resume"} {
		t.Run("Given a lost resume response, when the actor crashes during "+phase+", assert that cleanup stops and permits another message", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			abandonDispatch(t, store, session)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base, resumeAttempt: func(_ context.Context, call int32) error {
				if call == 1 {
					if phase == "resume" {
						setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_CRASHED)
					}
					return status.Error(codes.Unavailable, "resume response lost")
				}
				return nil
			}, suspendAttempt: func(context.Context, int32) error {
				setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_CRASHED)
				return status.Error(codes.FailedPrecondition, "actor crashed before suspension")
			}}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, workflow.quiesceAbandonedDispatch(ctx))

			require.Zero(t, writes.finishedCleanup.Load())
			require.EqualValues(t, 1, writes.cleanupClaims.Load())
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
			_, err := store.ClaimSessionDispatchCleanup(t.Context(), "")
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}
}

func TestAbandonedDispatchRecordsSuspensionDuringShutdown(t *testing.T) {
	// Given a cleanup whose successful suspension races worker shutdown.
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, afterSuspend: func(*ateapipb.Actor) {
		cancel()
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}

	// When shutdown occurs as the successful runtime response arrives, assert
	// that the known snapshot is recorded and admission opens for the next turn.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.EqualValues(t, 1, actors.suspendCalls.Load())
	require.EqualValues(t, 1, writes.finishedCleanup.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchRejectsMissingSnapshot(t *testing.T) {
	// Given a suspension response without the durable snapshot it promised.
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, afterSuspend: func(actor *ateapipb.Actor) {
		actor.Status.Snapshots = nil
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When cleanup validates the result, assert that it stops retrying the
	// invalid runtime response without blocking a new message.
	require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx))
	require.EqualValues(t, 1, actors.suspendCalls.Load())
	require.Zero(t, writes.finishedCleanup.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestSavedWaitingInputStillWakesDispatchCleanup(t *testing.T) {
	// Given a continuation whose input was saved without starting execution.
	store, actors, session := dispatchCleanupFixture(t)
	boundary := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateInputRequired)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, boundary.TaskId, boundary.Version))
	waiting, version, err := store.GetVersionedSessionTask(t.Context(), session.Id, boundary.TaskId)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	message.ContextID, message.TaskID = session.ContextId, waiting.ID
	dispatchID := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, ""))
	waiting.History = append(waiting.History, message)
	hash := sha256.Sum256([]byte(message.ID))
	_, err = store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], waiting, message, dispatchID.String())
	require.NoError(t, err)
	wake := make(chan struct{}, 1)
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
	startQuiescenceWorker(t, NewActorWorkflow(writes, actors, wake, time.Hour, noop.NewMeterProvider()))
	require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, time.Millisecond)
	service := NewInteractionService(store, nil, NewService(store, serviceTestAuthorizer{}, nil), false, false, wake)

	// When revocation finds that accepted input, it must still signal cleanup.
	agent := types.NamespacedName{Namespace: session.Agent.Ref.Namespace, Name: session.Agent.Ref.Name}
	notAccepted, err := service.RevokeSend(serviceTestContext(session.Creator), agent, message, dispatchID)
	require.NoError(t, err)
	require.False(t, notAccepted, "persisted input must not be reported safe to resend")

	// Assert that the actor suspends while the accepted waiting task survives.
	require.Eventually(t, func() bool { return writes.finishedCleanup.Load() == 1 }, 5*time.Second, time.Millisecond)
	stored, err := store.GetSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
	require.Equal(t, message.ID, stored.History[len(stored.History)-1].ID)
	actor, err := actors.GetActor(t.Context(), agent.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchReadFailureKeepsAdmissionOpen(t *testing.T) {
	// Given a due cleanup whose actor cannot currently be read.
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, readErr: status.Error(codes.Unavailable, "actor lookup unavailable")}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())

	// When an owned read fails, assert that its retry yields the worker and
	// a new delivery can supersede cleanup without waiting for that retry.
	cleanup, err := workflow.claimAbandonedDispatch(t.Context(), "")
	require.NoError(t, err)
	done, delay, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
	require.NoError(t, err)
	require.False(t, done)
	require.Positive(t, delay)
	require.Zero(t, actors.suspendCalls.Load())
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchCancellationDuringReadKeepsAdmissionOpen(t *testing.T) {
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	actors := &gatedQuiescenceActors{lifecycleTestActors: base, release: make(chan struct{}), calls: map[string]int{}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx)
	}()
	require.Eventually(t, func() bool { return actors.entered.Load() == 1 }, 5*time.Second, time.Millisecond)

	// Cancel the owner while it is reading the actor, before any mutation.
	// The canceled attempt must not close admission.
	cancel()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled cleanup preparation did not stop")
	}
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchCannotSuspendNewDelivery(t *testing.T) {
	for _, state := range []ateapipb.ActorState{ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_RESUMING} {
		t.Run("Given an actor in "+state.String()+", when a newer send wins before cleanup claims, assert that cleanup issues no runtime mutations", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, state)
			abandonDispatch(t, store, session)
			dispatchID := uuid.New()
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "next-message"))
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			require.NoError(t, workflow.quiesceAbandonedDispatch(t.Context()))

			require.Zero(t, actors.resumeCalls.Load())
			require.Zero(t, actors.suspendCalls.Load())
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next"))
			message.ID, message.ContextID = "next-message", session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			hash := sha256.Sum256([]byte(task.ID))
			_, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, dispatchID.String())
			require.NoError(t, err, "the winning delivery must still be able to accept its task")
		})
	}
}

func TestAbandonedDispatchRetriesKnownFailures(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		abortedCalls   int32
		finishFailures int32
	}{
		{name: "Substrate lease contention", abortedCalls: 2},
		{name: "database completion failure", finishFailures: 2},
	} {
		t.Run("Given "+scenario.name+", when cleanup runs, assert that one suspension completes", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			abandonDispatch(t, store, session)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base, suspendAttempt: func(_ context.Context, call int32) error {
				if call <= scenario.abortedCalls {
					return status.Error(codes.Aborted, "actor lease is held")
				}
				return nil
			}}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}, finishFailures: scenario.finishFailures}
			require.NoError(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(t.Context()))

			require.EqualValues(t, 1, writes.finishedCleanup.Load())
			require.Equal(t, scenario.abortedCalls+1, actors.suspendCalls.Load())
			require.Equal(t, scenario.finishFailures+1, writes.finishAttempts.Load())
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
		})
	}
}

func TestAbandonedDispatchSupersededAfterClaim(t *testing.T) {
	for _, state := range []ateapipb.ActorState{ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_RESUMING} {
		t.Run("Given claimed cleanup in "+state.String()+", when a new message arrives, assert that the old worker issues no runtime calls", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, state)
			abandonDispatch(t, store, session)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			cleanup, err := workflow.claimAbandonedDispatch(t.Context(), "")
			require.NoError(t, err)
			require.NotNil(t, cleanup)
			reads := actors.readCalls.Load()

			dispatchID := uuid.New()
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "new-message"))
			done, _, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
			require.NoError(t, err)
			require.True(t, done)
			require.Equal(t, reads, actors.readCalls.Load())
			require.Zero(t, actors.resumeCalls.Load())
			require.Zero(t, actors.suspendCalls.Load())

			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("new turn"))
			message.ID, message.ContextID = "new-message", session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			hash := sha256.Sum256([]byte(task.ID))
			_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, dispatchID.String())
			require.NoError(t, err, "stale cleanup must not consume the new dispatch")
		})
	}
}

func TestAbandonedDispatchRechecksAfterActorRead(t *testing.T) {
	store, base, session := dispatchCleanupFixture(t)
	abandonDispatch(t, store, session)
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, afterRead: func() {
		// A new delivery arrives during the owner's actor read, after the
		// attempt's first dispatch validation and before runtime mutation.
		require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "new-message"))
	}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	require.NoError(t, workflow.quiesceAbandonedDispatch(t.Context()))
	require.Zero(t, actors.resumeCalls.Load())
	require.Zero(t, actors.suspendCalls.Load(), "a message arriving during the read must supersede suspension")
}

func TestAbandonedDispatchNewMessageDuringRuntimeCall(t *testing.T) {
	for _, phase := range []string{"resume", "suspend"} {
		t.Run("Given cleanup issuing "+phase+", when a new message arrives before the call returns, assert that cleanup stops", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			abandonDispatch(t, store, session)
			dispatchID := uuid.New()
			arrive := func(context.Context, int32) error {
				require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "new-message"))
				return nil // The already-issued runtime call is allowed to finish.
			}
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			if phase == "resume" {
				actors.resumeAttempt = arrive
			} else {
				actors.suspendAttempt = arrive
			}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			require.NoError(t, workflow.quiesceAbandonedDispatch(t.Context()))
			require.EqualValues(t, 1, actors.resumeCalls.Load())
			if phase == "resume" {
				require.Zero(t, actors.suspendCalls.Load())
			} else {
				require.EqualValues(t, 1, actors.suspendCalls.Load())
			}
			require.Zero(t, writes.finishedCleanup.Load(), "the obsolete snapshot must not be published")

			// The normal runtime flow resumes if the old suspension completed.
			_, err := base.ResumeActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
			require.NoError(t, err)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("new turn"))
			message.ID, message.ContextID = "new-message", session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			hash := sha256.Sum256([]byte(task.ID))
			_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, dispatchID.String())
			require.NoError(t, err)
		})
	}
}

func TestAbandonedDispatchNewMessageStopsBackoff(t *testing.T) {
	for _, phase := range []string{"resume", "suspend", "snapshot write", "park write"} {
		t.Run("Given cleanup retrying "+phase+", when a new message arrives, assert that no more runtime calls are made", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			switch phase {
			case "resume":
				setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			case "park write":
				setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
			}
			abandonDispatch(t, store, session)
			fail := func(context.Context, int32) error { return status.Error(codes.Unavailable, "temporary failure") }
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			switch phase {
			case "resume":
				actors.resumeAttempt = fail
			case "suspend":
				actors.suspendAttempt = fail
			case "snapshot write":
				writes.finishFailures = 1
			case "park write":
				writes.parkFailures = 1
			}
			workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			cleanup, err := workflow.claimAbandonedDispatch(t.Context(), "")
			require.NoError(t, err)
			require.NotNil(t, cleanup)
			done, delay, err := workflow.retryDispatchCleanup(t.Context(), cleanup)
			require.NoError(t, err)
			require.False(t, done)
			require.Positive(t, delay)
			reads, resumes, suspends := actors.readCalls.Load(), actors.resumeCalls.Load(), actors.suspendCalls.Load()

			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "new-message"))
			done, _, err = workflow.retryDispatchCleanup(t.Context(), cleanup)
			require.NoError(t, err)
			require.True(t, done)
			require.Equal(t, reads, actors.readCalls.Load())
			require.Equal(t, resumes, actors.resumeCalls.Load())
			require.Equal(t, suspends, actors.suspendCalls.Load())
			require.Zero(t, writes.finishedCleanup.Load())
		})
	}
}

func TestAbandonedDispatchShutdownAllowsNewMessage(t *testing.T) {
	for _, phase := range []string{"resume", "suspend"} {
		t.Run("Given an uncertain "+phase+" call, when the worker shuts down, assert that a new message supersedes cleanup", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
			abandonDispatch(t, store, session)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			entered := make(chan struct{})
			mutation := base.SuspendActor
			if phase == "resume" {
				mutation = base.ResumeActor
			}
			interrupted := func(ctx context.Context, _ int32) error {
				if _, err := mutation(ctx, session.Agent.Ref.Namespace, substrate.ActorName(session.Id)); err != nil {
					return err
				}
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}
			if phase == "resume" {
				actors.resumeAttempt = interrupted
			} else {
				actors.suspendAttempt = interrupted
			}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).quiesceAbandonedDispatch(ctx)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup did not reach the runtime mutation")
			}
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not stop cleanup")
			}

			require.EqualValues(t, 1, writes.cleanupClaims.Load())
			require.Zero(t, writes.finishedCleanup.Load())
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
			_, err := store.ClaimSessionDispatchCleanup(t.Context(), "")
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}
}

func TestAbandonedDispatchRestartFinishesSuspension(t *testing.T) {
	for _, state := range []ateapipb.ActorState{ateapipb.ActorState_ACTOR_STATE_SUSPENDING, ateapipb.ActorState_ACTOR_STATE_SUSPENDED} {
		t.Run("Given an expired owner with an actor in "+state.String()+", when a replacement worker runs, assert that it finishes suspension without resuming", func(t *testing.T) {
			store, base, session := dispatchCleanupFixture(t)
			abandonDispatch(t, store, session)
			attemptCtx, cancel := context.WithTimeout(t.Context(), database.RuntimeOperationTimeout)
			defer cancel()
			claimed, err := store.ClaimSessionDispatchCleanup(attemptCtx, "")
			require.NoError(t, err)
			require.NoError(t, store.ContinueSessionDispatchCleanup(attemptCtx, claimed, true))
			// Simulate a lost response or unfinished call after durable phase
			// advancement, then the original process disappearing.
			_, err = base.SuspendActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
			require.NoError(t, err)
			setDispatchCleanupActorState(base, session, state)
			cancel()
			makeDispatchCleanupDue(t, store, claimed)
			actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
			writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}
			replacement := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
			pending, err := replacement.claimAbandonedDispatch(t.Context(), "")
			require.NoError(t, err)
			require.True(t, pending.work.Suspending)
			require.NoError(t, replacement.finishDispatchCleanup(t.Context(), pending))
			require.Zero(t, actors.resumeCalls.Load())
			require.EqualValues(t, 1, actors.suspendCalls.Load())
			require.EqualValues(t, 1, writes.finishedCleanup.Load())
			require.ErrorIs(t, store.ContinueSessionDispatchCleanup(t.Context(), claimed, true), database.ErrConflict)
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "new-message"))
		})
	}
}

// A new canceled message can race an old cleanup's already-issued suspension.
// Its interrupted resume must not be claimed by the prior turn's task worker.
func TestAbandonedDispatchRecoversWithPendingTaskSuspension(t *testing.T) {
	// Given a completed turn and cleanup for a later abandoned delivery.
	store, base, session := dispatchCleanupFixture(t)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
	abandonDispatch(t, store, session)
	const secondMessage = "second-canceled-message"
	secondDispatch := uuid.New()
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base}
	actors.suspendAttempt = func(ctx context.Context, call int32) error {
		if call == 1 {
			return store.ReserveSessionDispatch(ctx, session.Id, secondDispatch, secondMessage)
		}
		actor, err := base.GetActor(ctx, session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
		require.NoError(t, err)
		if actor.Status.State == ateapipb.ActorState_ACTOR_STATE_RESUMING {
			return status.Error(codes.FailedPrecondition, "cannot suspend a resuming actor")
		}
		return nil
	}
	actors.afterSuspend = func(*ateapipb.Actor) {
		// The newer message resumes after the issued suspension completes,
		// then is canceled before restore finishes or a turn starts.
		setDispatchCleanupActorState(base, session, ateapipb.ActorState_ACTOR_STATE_RESUMING)
		revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, secondDispatch, secondMessage)
		require.NoError(t, err)
		require.True(t, revoked)
	}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	require.NoError(t, workflow.quiesceAbandonedDispatch(t.Context()))
	actors.afterSuspend = nil

	// When workers revisit the session, assert that only dispatch cleanup
	// owns suspension and can first finish the interrupted resume.
	_, err := store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound)
	pending, err := workflow.claimAbandonedDispatch(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, secondDispatch, pending.work.DispatchID)
	require.NoError(t, workflow.finishDispatchCleanup(t.Context(), pending))
	require.EqualValues(t, 1, actors.resumeCalls.Load())
	require.EqualValues(t, 2, actors.suspendCalls.Load())
	actor, err := base.GetActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	_, version, err := store.GetVersionedSessionTask(t.Context(), session.Id, request.TaskId)
	require.NoError(t, err)
	require.Equal(t, request.Version, version, "cleanup must not add a turn or task event")
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next-message"))
}

func TestAbandonedDispatchSlowReadDoesNotOccupyOtherWorkers(t *testing.T) {
	// Given one abandoned actor whose lookup blocks and another completed turn.
	store, base, abandoned := dispatchCleanupFixture(t)
	abandonDispatch(t, store, abandoned)
	healthy, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: abandoned.Agent}, uuid.NewString())
	require.NoError(t, err)
	creator := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider())
	healthy, err = creator.Create(t.Context(), healthy)
	require.NoError(t, err)
	_, err = base.ResumeActor(t.Context(), healthy.Agent.Ref.Namespace, substrate.ActorName(healthy.Id))
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, healthy, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), healthy.Id, request.TaskId, request.Version))
	release := make(chan struct{})
	defer close(release)
	var blocked atomic.Int32
	actors := &dispatchCleanupTestActors{lifecycleTestActors: base, readAttempt: func(ctx context.Context, name string) error {
		if name != substrate.ActorName(abandoned.Id) {
			return nil
		}
		blocked.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	writes := &observedDispatchCleanupStore{observedQuiescenceStore: &observedQuiescenceStore{lifecycleTestStore: store}}

	// When the worker pool starts, assert that the healthy turn can suspend
	// while only one worker waits on the abandoned actor's lookup.
	startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour, noop.NewMeterProvider()))
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 && blocked.Load() > 0 }, 5*time.Second, time.Millisecond)
	require.EqualValues(t, 1, blocked.Load())
	require.EqualValues(t, 1, writes.cleanupClaims.Load())
	require.Zero(t, writes.finishedCleanup.Load())
	actor, err := base.GetActor(t.Context(), healthy.Agent.Ref.Namespace, substrate.ActorName(healthy.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), abandoned.Id, uuid.New(), "next-message"))
}

func dispatchCleanupFixture(t *testing.T) (*lifecycleTestStore, *lifecycleTestActors, *apiv1.Session) {
	t.Helper()
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, noop.NewMeterProvider()).Create(t.Context(), session)
	require.NoError(t, err)
	_, err = actors.ResumeActor(t.Context(), session.Agent.Ref.Namespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	return store, actors, session
}

func setDispatchCleanupActorState(actors *lifecycleTestActors, session *apiv1.Session, state ateapipb.ActorState) {
	actors.mu.Lock()
	defer actors.mu.Unlock()
	actors.actors[actorKey(session.Agent.Ref.Namespace, substrate.ActorName(session.Id))].Status.State = state
}

func abandonDispatch(t *testing.T, store *lifecycleTestStore, session *apiv1.Session) *database.SessionDispatchCleanup {
	t.Helper()
	dispatchID := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "abandoned-message"))
	notAccepted, err := store.RevokeSessionDispatch(t.Context(), session.Id, dispatchID, "abandoned-message")
	require.NoError(t, err)
	require.True(t, notAccepted)
	return &database.SessionDispatchCleanup{Session: session, DispatchID: dispatchID}
}

type observedDispatchCleanupStore struct {
	*observedQuiescenceStore
	cleanupClaims   atomic.Int32
	finishedCleanup atomic.Int32
	finishAttempts  atomic.Int32
	finishFailures  int32
	parkedCleanup   atomic.Int32
	parkAttempts    atomic.Int32
	parkFailures    int32
}

var _ workflowStore = (*observedDispatchCleanupStore)(nil)

func (s *observedDispatchCleanupStore) ClaimSessionDispatchCleanup(ctx context.Context, afterSessionID string) (*database.SessionDispatchCleanup, error) {
	work, err := s.Client.ClaimSessionDispatchCleanup(ctx, afterSessionID)
	if err == nil {
		s.cleanupClaims.Add(1)
	}
	return work, err
}

func (s *observedDispatchCleanupStore) FinishSessionDispatchCleanup(ctx context.Context, work *database.SessionDispatchCleanup, snapshot *database.SessionTaskSnapshot) error {
	if s.finishAttempts.Add(1) <= s.finishFailures {
		return status.Error(codes.Unavailable, "database unavailable")
	}
	err := s.Client.FinishSessionDispatchCleanup(ctx, work, snapshot)
	if err == nil {
		s.finishedCleanup.Add(1)
	}
	return err
}

func (s *observedDispatchCleanupStore) ParkSessionDispatchCleanup(ctx context.Context, work *database.SessionDispatchCleanup) error {
	if s.parkAttempts.Add(1) <= s.parkFailures {
		return status.Error(codes.Unavailable, "database unavailable")
	}
	err := s.Client.ParkSessionDispatchCleanup(ctx, work)
	if err == nil {
		s.parkedCleanup.Add(1)
	}
	return err
}

// makeDispatchCleanupDue simulates elapsed database time without waiting for
// the production parked interval. The generation guard preserves other jobs.
func makeDispatchCleanupDue(t *testing.T, store *lifecycleTestStore, work *database.SessionDispatchCleanup) {
	t.Helper()
	tag, err := store.pool.Exec(t.Context(), `
		UPDATE runtime_lifecycle_job SET available_at = clock_timestamp() - interval '1 second'
		WHERE runtime_id = $1 AND kind = $2 AND generation = $3
	`, work.Session.Id, database.RuntimeLifecycleJobRecoverAbandonedDispatch, work.DispatchID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

type gatedDispatchDeletionActors struct {
	*dispatchCleanupTestActors
	deleteEntered chan struct{}
	releaseDelete <-chan struct{}
}

func (a *gatedDispatchDeletionActors) DeleteActor(ctx context.Context, space, name, uid string) error {
	close(a.deleteEntered)
	select {
	case <-a.releaseDelete:
		return a.lifecycleTestActors.DeleteActor(ctx, space, name, uid)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type dispatchCleanupTestActors struct {
	*lifecycleTestActors
	afterRead       func()
	readAttempt     func(context.Context, string) error
	resumeAttempt   func(context.Context, int32) error
	suspendAttempt  func(context.Context, int32) error
	afterSuspend    func(*ateapipb.Actor)
	resumedActorUID string
	readErr         error
	readCalls       atomic.Int32
	resumeCalls     atomic.Int32
	suspendCalls    atomic.Int32
}

var _ actorClient = (*dispatchCleanupTestActors)(nil)

func (a *dispatchCleanupTestActors) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	a.readCalls.Add(1)
	if a.readAttempt != nil {
		if err := a.readAttempt(ctx, name); err != nil {
			return nil, err
		}
	}
	if a.readErr != nil {
		return nil, a.readErr
	}
	actor, err := a.lifecycleTestActors.GetActor(ctx, space, name)
	if a.afterRead != nil {
		a.afterRead()
	}
	return actor, err
}

func (a *dispatchCleanupTestActors) SuspendActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	call := a.suspendCalls.Add(1)
	if a.suspendAttempt != nil {
		if err := a.suspendAttempt(ctx, call); err != nil {
			return nil, err
		}
	}
	actor, err := a.lifecycleTestActors.SuspendActor(ctx, space, name)
	if err == nil && a.afterSuspend != nil {
		a.afterSuspend(actor)
	}
	return actor, err
}

func (a *dispatchCleanupTestActors) ResumeActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	call := a.resumeCalls.Add(1)
	if a.resumeAttempt != nil {
		if err := a.resumeAttempt(ctx, call); err != nil {
			return nil, err
		}
	}
	actor, err := a.lifecycleTestActors.ResumeActor(ctx, space, name)
	if err == nil && a.resumedActorUID != "" {
		actor.Metadata.Uid = a.resumedActorUID
	}
	return actor, err
}
