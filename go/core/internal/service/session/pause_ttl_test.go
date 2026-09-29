package session

import (
	"context"
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
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
	work, err := store.ClaimSessionQuiescence(t.Context(), 0)
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
	work, err := store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL)
	require.NoError(t, err)
	require.True(t, work.Suspend)
	workflow.quiesceIdleSession(t.Context(), work)
	require.EqualValues(t, 1, actors.suspends.Load())
	require.EqualValues(t, 1, actors.pauses.Load(), "an expired pause is suspended, not paused again")
	for _, actor := range actors.actors {
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	}
	_, err = store.ClaimSessionQuiescence(t.Context(), workflow.pausedRuntimeTTL)
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
	_, err = store.ClaimSessionQuiescence(t.Context(), NewActorWorkflow(store, actors).pausedRuntimeTTL)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Zero(t, actors.suspends.Load())
}
