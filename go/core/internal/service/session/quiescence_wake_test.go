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
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
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

func settlementContext(t *testing.T, session *apiv1alpha1.Session) context.Context {
	t.Helper()
	headers := http.Header{}
	headers.Set(apia2a.InsecureRuntimeIdentityHeader, "team-a/"+substrate.ActorName(session.Id)+"/actor-uid")
	identity, err := (&taskstore.Authenticator{}).Authenticate(t.Context(), headers, nil)
	require.NoError(t, err)
	return auth.AuthSessionTo(t.Context(), identity)
}

func stageQuiescenceTask(t *testing.T, store *database.Client, session *apiv1alpha1.Session, state a2a.TaskState) *apiv1alpha1.TaskStoreServiceSettleTaskRequest {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = state
	hash := sha256.Sum256([]byte(task.ID))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	return &apiv1alpha1.TaskStoreServiceSettleTaskRequest{SessionId: session.Id, TaskId: string(task.ID), Version: version}
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
	creator := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour)
	var sessions []*apiv1alpha1.Session
	var requests []*apiv1alpha1.TaskStoreServiceSettleTaskRequest
	states := []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired}
	for i := range 36 {
		session := first
		if i != 0 {
			var err error
			session, _, err = store.CreateSession(t.Context(), &apiv1alpha1.Session{Id: uuid.NewString(), Creator: "alice", Agent: first.Agent}, uuid.NewString())
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
		workflow := NewActorWorkflow(writes, runtime, wake, time.Hour)
		startQuiescenceWorker(t, workflow)
		workflows = append(workflows, workflow)
		services = append(services, taskstore.NewService(store, wake, nil))
	}
	require.Eventually(t, func() bool { return writes.idle.Load() >= 8 }, 5*time.Second, 10*time.Millisecond)
	// Failed settlement leaves the task unpublished and no eligible work.
	invalid := &apiv1alpha1.TaskStoreServiceSettleTaskRequest{SessionId: requests[0].SessionId, TaskId: requests[0].TaskId, Version: requests[0].Version + 1}
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
		if states[i%len(states)].Terminal() {
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
			_, snapshot, err := store.ReserveSessionCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: requests[i].TaskId}, "alice", uuid.NewString())
			require.NoError(t, err)
			require.Equal(t, "s3://snapshots/snapshot-1", snapshot.URI)
		} else {
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_PAUSED, actor.Status.State)
		}
	}
}

func TestQuiescenceRecoversMissedSignal(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour).Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateInputRequired)
	writes := &observedQuiescenceStore{lifecycleTestStore: store}
	startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), 100*time.Millisecond))
	require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, time.Millisecond)
	// This simulates a commit on a replica that dies before sending its hint.
	_, err = taskstore.NewService(store, nil, nil).SettleTask(settlementContext(t, session), request)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
}

func TestQuiescenceRetriesTemporaryBlockers(t *testing.T) {
	for _, blocker := range []string{"dispatch", "lifecycle"} {
		t.Run(blocker, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour).Create(t.Context(), session)
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
				operation, err := store.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
				require.NoError(t, err)
				unblock = func() {
					_, err := store.FinishSessionOperation(t.Context(), session.Id, operation.ID, uuid.Nil, "", "", "preparation failed before runtime I/O")
					require.NoError(t, err)
				}
			}
			writes := &observedQuiescenceStore{lifecycleTestStore: store}
			startQuiescenceWorker(t, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour))
			require.Eventually(t, func() bool { return writes.idle.Load() >= 4 }, 5*time.Second, 10*time.Millisecond)
			require.Zero(t, writes.finished.Load())
			pending, err := store.HasPendingSessionQuiescence(t.Context())
			require.NoError(t, err)
			require.True(t, pending, "an empty claim must distinguish blocked work from idle")
			unblock() // No signal: retry must discover the cleared blocker promptly.
			require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
		})
	}
}

func TestQuiescenceResumesPendingWorkAfterExplicitSuspend(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	writes := &observedQuiescenceStore{lifecycleTestStore: store}
	workflow := NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Hour)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateInputRequired)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
	session, err = workflow.Suspend(t.Context(), session)
	require.NoError(t, err)
	pending, err := store.HasPendingSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.False(t, pending, "suspended actors must not keep fast retries active")
	startQuiescenceWorker(t, workflow)
	// Creation's buffered wake-up adds an extra empty scan.
	require.Eventually(t, func() bool { return writes.idle.Load() >= 5 }, 5*time.Second, 10*time.Millisecond)
	_, err = workflow.Resume(t.Context(), session)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return writes.finished.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
}

func TestQuiescenceCancellationRetainsClaim(t *testing.T) {
	store, session := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour).Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
	actors := &gatedQuiescenceActors{lifecycleTestActors: base, release: make(chan struct{}), calls: map[string]int{}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Hour).Start(ctx) }()
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
