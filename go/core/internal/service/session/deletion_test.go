package session

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestDeleteSessionWithInterruptedResume(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &deletionTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_RESUMING

	deleted, err := workflow.Delete(t.Context(), session, false)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	require.Zero(t, actors.suspends)
	require.Equal(t, "actor-uid", actors.deletedUID)
	_, err = actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestForceDeletionRetriesWithoutForceAfterInterruption(t *testing.T) {
	scenarios := []struct {
		name            string
		failAfterDelete bool
		failCompletion  bool
	}{
		{name: "failed actor deletion"},
		{name: "a lost actor deletion response", failAfterDelete: true},
		{name: "failed database completion after actor deletion", failCompletion: true},
	}
	for _, scenario := range scenarios {
		t.Run("Given "+scenario.name+", when a normal retry follows force deletion, assert that deletion completes", func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &deletionTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("keep working"))
			input.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(input, input)
			task.Status.State = a2a.TaskStateWorking
			digest := sha256.Sum256([]byte(uuid.NewString()))
			_, err = store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
			require.NoError(t, err)
			_, err = workflow.Delete(t.Context(), session, false)
			require.ErrorIs(t, err, database.ErrConflict)
			require.Empty(t, actors.deletedUID, "normal deletion must not contact the backend during a turn")

			actors.failDelete, actors.failAfterDelete = !scenario.failCompletion, scenario.failAfterDelete
			if scenario.failCompletion {
				workflow = NewActorWorkflow(&completionTestStore{
					lifecycleTestStore: store,
					finishErr:          status.Error(codes.Unavailable, "database completion unavailable"),
				}, actors, make(chan struct{}, 1), time.Second)
			}
			_, err = workflow.Delete(t.Context(), session, true)
			require.Error(t, err)
			pending, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, pending.State)
			working, err := store.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateWorking, working.Status.State)
			restarted := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			deleted, err := restarted.Delete(t.Context(), pending, false)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
			require.Zero(t, actors.suspends)
			_, err = store.GetSessionByID(t.Context(), session.Id)
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}
}

func TestDeletionPreservesActorReplacedAfterPreparation(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		force bool
	}{
		{name: "normal"},
		{name: "forced", force: true},
	} {
		t.Run("Given a replacement after preparation, when "+scenario.name+" deletion runs, assert that the replacement survives", func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			name := substrate.ActorName(session.Id)
			replacingStore := &completionTestStore{lifecycleTestStore: store, afterClaim: func(context.Context) {
				actors.mu.Lock()
				defer actors.mu.Unlock()
				actors.actors[actorKey("team-a", name)].Metadata.Uid = "replacement-uid"
			}}

			_, err = NewActorWorkflow(replacingStore, actors, make(chan struct{}, 1), time.Second).Delete(t.Context(), session, scenario.force)
			require.Equal(t, codes.FailedPrecondition, status.Code(err))
			replacement, err := actors.GetActor(t.Context(), "team-a", name)
			require.NoError(t, err)
			require.Equal(t, "replacement-uid", replacement.GetMetadata().GetUid())
			pending, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, pending.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, pending.Operation)
			require.NotEmpty(t, pending.PreparedRevision, "failed deletion must retain its runtime references")
		})
	}
}

type deletionTestActors struct {
	*lifecycleTestActors
	suspends        int
	deletedUID      string
	failDelete      bool
	failAfterDelete bool
}

func (a *deletionTestActors) SuspendActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.suspends++
	return nil, status.Error(codes.FailedPrecondition, "cannot suspend a resuming actor")
}

func (a *deletionTestActors) DeleteActor(ctx context.Context, atespace, name, uid string) error {
	a.deletedUID = uid
	fail := a.failDelete
	a.failDelete = false
	if fail && !a.failAfterDelete {
		return status.Error(codes.Unavailable, "termination unavailable")
	}
	if err := a.lifecycleTestActors.DeleteActor(ctx, atespace, name, uid); err != nil {
		return err
	}
	if fail {
		return status.Error(codes.Unavailable, "deletion response lost")
	}
	return nil
}
