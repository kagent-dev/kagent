package session

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
)

// A turn silent for longer than the timeout ends as FAILED with the
// interruption, and the session takes its next message. When the Actor is
// crashed or gone as well, the session records the lost runtime instead.
func TestStalledTurnSweepEndsTheTurn(t *testing.T) {
	for _, test := range []struct {
		name  string
		actor ateapipb.ActorState
		gone  bool
		lost  bool
	}{
		{name: "runtime alive", actor: ateapipb.ActorState_ACTOR_STATE_RUNNING},
		{name: "runtime crashed", actor: ateapipb.ActorState_ACTOR_STATE_CRASHED, lost: true},
		{name: "runtime gone", gone: true, lost: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			key := actorKey("team-a", substrate.ActorName(session.Id))
			if test.gone {
				delete(actors.actors, key)
			} else {
				actors.actors[key].Status.State = test.actor
			}
			task := workingTaskFixture(t, store, session)
			worker, err := NewStalledTurnWorker(store, NewService(store, &serviceTestAuthorizer{}, workflow), time.Hour, time.Minute)
			require.NoError(t, err)

			worker.sweep(t.Context(), time.Now())
			current, err := store.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateWorking, current.Status.State, "a turn younger than the timeout is left alone")

			worker.sweep(t.Context(), time.Now().Add(2*time.Hour))
			ended, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, ended.Status.State)
			require.Equal(t, a2a.NewTextPart("turn interrupted: the runtime recorded no progress for 1h0m0s"), ended.Status.Message.Parts[0])
			after, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			if test.lost {
				require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, after.State)
				require.Equal(t, apia2a.FailureReasonRuntimeLost, after.GetFailure().GetReason())
				require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), database.ErrConflict)
			} else {
				require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, after.State)
				require.Nil(t, after.GetFailure())
				require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"))
			}
		})
	}
}

// A boundary the runtime saved but never settled is the runtime's own outcome:
// the sweep publishes it and leaves the session's runtime state alone.
func TestStalledTurnSweepSettlesASavedOutcome(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	task := workingTaskFixture(t, store, session)
	_, version, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	done := *task
	done.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	hash := sha256.Sum256([]byte("done"))
	_, err = store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], &done, &done, "")
	require.NoError(t, err)

	worker, err := NewStalledTurnWorker(store, NewService(store, &serviceTestAuthorizer{}, workflow), time.Hour, time.Minute)
	require.NoError(t, err)
	worker.sweep(t.Context(), time.Now().Add(2*time.Hour))
	settled, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, settled.Status.State)
	after, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, after.State, "a settled outcome is not a lost runtime")
}

func workingTaskFixture(t *testing.T, store *lifecycleTestStore, session *apiv1alpha1.Session) *a2a.Task {
	t.Helper()
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = a2a.TaskStateWorking
	hash := sha256.Sum256([]byte("working"))
	_, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	return task
}
