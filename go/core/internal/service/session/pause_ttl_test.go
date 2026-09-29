package session

import (
	"context"
	"crypto/sha256"
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

// countingActors counts the pause and suspend calls of the fake actor client.
type countingActors struct {
	*lifecycleTestActors
	pauses, suspends atomic.Int32
}

func (a *countingActors) PauseActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.pauses.Add(1)
	return a.lifecycleTestActors.PauseActor(ctx, atespace, name)
}

func (a *countingActors) SuspendActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.suspends.Add(1)
	return a.lifecycleTestActors.SuspendActor(ctx, atespace, name)
}

// pausedSessionFixture leaves a session paused on a waiting task through the
// idle worker's own pause, with the boundary's age set for the TTL check.
func pausedSessionFixture(t *testing.T, store *lifecycleTestStore, actors actorClient, session *apiv1alpha1.Session, age time.Duration) *a2a.Task {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("Which database?"))}
	hash := sha256.Sum256([]byte("waiting"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	work, err := store.ClaimSessionQuiescence(t.Context(), 0, nil)
	require.NoError(t, err)
	require.False(t, work.Suspend)
	workflow.quiesceIdleSession(t.Context(), work)
	_, err = store.pool.Exec(t.Context(), `
		UPDATE session_task_event SET created_at = created_at - $2::interval
		WHERE history_id = (SELECT history_id FROM session WHERE id = $1)
	`, session.Id, age)
	require.NoError(t, err)
	return task
}

// A pause older than the TTL is suspended durably: the Actor moves from PAUSED
// to SUSPENDED and the waiting task records the snapshot, so a reply after the
// pause's node is gone restores the runtime elsewhere.
func TestIdleWorkerSuspendsAnExpiredPause(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &countingActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	task := pausedSessionFixture(t, store, actors, session, time.Hour)
	require.EqualValues(t, 1, actors.pauses.Load())
	for _, actor := range actors.actors {
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_PAUSED, actor.GetStatus().GetState())
	}

	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, WithPausedRuntimeTTL(2*time.Minute))
	work, err := store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL, nil)
	require.NoError(t, err)
	require.True(t, work.Suspend)
	workflow.quiesceIdleSession(t.Context(), work)
	require.EqualValues(t, 1, actors.suspends.Load())
	require.EqualValues(t, 1, actors.pauses.Load(), "an expired pause is suspended, not paused again")
	for _, actor := range actors.actors {
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	}
	_, err = store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL, nil)
	require.ErrorIs(t, err, database.ErrNotFound, "the recorded snapshot ends the pause TTL's interest")
	waiting, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, waiting.Status.State)
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "reply"))
}

// Without a TTL the idle worker leaves an old pause in place.
func TestIdleWorkerLeavesAPauseWithoutATTL(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &countingActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	pausedSessionFixture(t, store, actors, session, time.Hour)
	_, err = store.ClaimSessionQuiescence(t.Context(), NewActorWorkflow(store, actors).pausedRuntimeTTL, nil)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Zero(t, actors.suspends.Load())
}

func TestPauseNodeLost(t *testing.T) {
	session := &apiv1alpha1.Session{Id: "session-1", PreparedRevision: "revision-1"}
	store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision"}}
	active := &ateapipb.Worker{NodeName: "node-a", Status: &ateapipb.WorkerStatus{State: ateapipb.WorkerState_WORKER_STATE_ACTIVE}}
	draining := &ateapipb.Worker{NodeName: "node-a", Status: &ateapipb.WorkerStatus{State: ateapipb.WorkerState_WORKER_STATE_DRAINING}}
	elsewhere := &ateapipb.Worker{NodeName: "node-b", Status: &ateapipb.WorkerStatus{State: ateapipb.WorkerState_WORKER_STATE_ACTIVE}}
	for name, test := range map[string]struct {
		state   ateapipb.ActorState
		local   *ateapipb.LocalSnapshot
		workers []*ateapipb.Worker
		lost    bool
	}{
		"worker on the node":  {state: ateapipb.ActorState_ACTOR_STATE_PAUSED, local: &ateapipb.LocalSnapshot{NodeName: "node-a"}, workers: []*ateapipb.Worker{elsewhere, active}},
		"node gone":           {state: ateapipb.ActorState_ACTOR_STATE_PAUSED, local: &ateapipb.LocalSnapshot{NodeName: "node-a"}, workers: []*ateapipb.Worker{elsewhere}, lost: true},
		"node draining":       {state: ateapipb.ActorState_ACTOR_STATE_PAUSED, local: &ateapipb.LocalSnapshot{NodeName: "node-a"}, workers: []*ateapipb.Worker{draining}, lost: true},
		"no worker at all":    {state: ateapipb.ActorState_ACTOR_STATE_PAUSED, local: &ateapipb.LocalSnapshot{NodeName: "node-a"}, lost: true},
		"durable copy exists": {state: ateapipb.ActorState_ACTOR_STATE_PAUSED, local: &ateapipb.LocalSnapshot{NodeName: "node-a", DurableCopy: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/pause"}}},
		"no node recorded":    {state: ateapipb.ActorState_ACTOR_STATE_PAUSED},
		"not paused":          {state: ateapipb.ActorState_ACTOR_STATE_RUNNING, local: &ateapipb.LocalSnapshot{NodeName: "node-a"}},
	} {
		t.Run(name, func(t *testing.T) {
			actors := &lifecycleTestActors{workers: test.workers, actors: map[string]*ateapipb.Actor{
				actorKey("team-a", substrate.ActorName(session.Id)): {
					Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: substrate.ActorName(session.Id), Uid: "actor-uid"},
					ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "assistant-kagent-revision"},
					Status:        &ateapipb.ActorStatus{State: test.state, LocalSnapshot: test.local},
				},
			}}
			lost, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).PauseNodeLost(t.Context(), session)
			require.NoError(t, err)
			require.Equal(t, test.lost, lost)
		})
	}
	t.Run("workers unknown", func(t *testing.T) {
		actors := &lifecycleTestActors{workersErr: status.Error(codes.Unavailable, "ate-api is rolling"), actors: map[string]*ateapipb.Actor{
			actorKey("team-a", substrate.ActorName(session.Id)): {
				Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: substrate.ActorName(session.Id), Uid: "actor-uid"},
				ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "assistant-kagent-revision"},
				Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_PAUSED, LocalSnapshot: &ateapipb.LocalSnapshot{NodeName: "node-a"}},
			},
		}}
		_, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).PauseNodeLost(t.Context(), session)
		require.ErrorIs(t, err, status.Error(codes.Unavailable, "ate-api is rolling"))
	})
}

// An expired pause whose checkpoint node has no worker is left alone: the
// claim is released so a reply is admitted, no suspend is issued, and the
// session sits out the next claims for one TTL.
func TestIdleWorkerLeavesAnExpiredPauseOnALostNode(t *testing.T) {
	for name, workersErr := range map[string]error{"node gone": nil, "workers unknown": status.Error(codes.Unavailable, "ate-api is rolling")} {
		t.Run(name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &countingActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
			session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			pausedSessionFixture(t, store, actors, session, time.Hour)
			actors.workersErr = workersErr
			for _, actor := range actors.actors {
				actor.Status.LocalSnapshot = &ateapipb.LocalSnapshot{NodeName: "node-a"}
			}

			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, WithPausedRuntimeTTL(2*time.Minute))
			work, err := store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL, workflow.deferred.active(time.Now()))
			require.NoError(t, err)
			require.True(t, work.Suspend)
			workflow.quiesceIdleSession(t.Context(), work)
			require.Zero(t, actors.suspends.Load())
			for _, actor := range actors.actors {
				require.Equal(t, ateapipb.ActorState_ACTOR_STATE_PAUSED, actor.GetStatus().GetState())
			}
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "reply"), "the released claim admits the reply")
			skip := workflow.deferred.active(time.Now())
			require.Equal(t, []string{session.Id}, skip)
			_, err = store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL, skip)
			require.ErrorIs(t, err, database.ErrNotFound, "a deferred session is not claimed again")
		})
	}
}

func TestDeferralsExpire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var deferred deferrals
		deferred.add("b", time.Now().Add(2*time.Minute))
		deferred.add("a", time.Now().Add(time.Minute))
		require.Equal(t, []string{"a", "b"}, deferred.active(time.Now()))
		time.Sleep(time.Minute)
		require.Equal(t, []string{"b"}, deferred.active(time.Now()))
		time.Sleep(time.Minute)
		require.Empty(t, deferred.active(time.Now()))
		require.Empty(t, deferred.until)
	})
}
