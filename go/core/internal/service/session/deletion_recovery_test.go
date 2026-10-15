package session

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// An adapter can die after saving a boundary and before SettleTask. Explicit
// deletion must remain available without claiming that the task completed.
func TestDeleteSessionWithUnpublishedRuntimeBoundary(t *testing.T) {
	for _, state := range []a2a.TaskState{
		a2a.TaskStateCompleted, a2a.TaskStateCanceled, a2a.TaskStateFailed,
		a2a.TaskStateRejected, a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired,
	} {
		for _, firstEvent := range []bool{false, true} {
			name := string(state) + "/update"
			if firstEvent {
				name = string(state) + "/create"
			}
			t.Run(name, func(t *testing.T) {
				store, session := lifecycleFixture(t)
				actors := &deletionFaultActors{
					lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}},
					deleteBeforeError:   firstEvent,
					readFailure:         state == a2a.TaskStateFailed,
					suspendFailure:      state == a2a.TaskStateCanceled,
				}
				workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
				session, err := workflow.Create(t.Context(), session)
				require.NoError(t, err)
				name := substrate.ActorName(session.Id)
				actor, err := actors.ResumeActor(t.Context(), "team-a", name)
				require.NoError(t, err)
				message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))
				message.ContextID = session.ContextId
				task := a2a.NewSubmittedTask(message, message)
				require.NoError(t, store.RegisterSessionPushNotification(t.Context(), session.Id, message.ID, "", &a2a.PushConfig{ID: "callback", URL: "http://receiver"}))
				var version int64
				if !firstEvent {
					digest := sha256.Sum256([]byte("start"))
					version, err = store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
					require.NoError(t, err)
				}
				task.Status = a2a.TaskStatus{State: state, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("saved boundary"))}
				digest := sha256.Sum256([]byte("boundary"))
				var boundaryVersion int64
				if firstEvent {
					boundaryVersion, err = store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
				} else {
					boundaryVersion, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
				}
				require.NoError(t, err)

				_, err = store.BeginIdleSessionDeletion(t.Context(), session.Id, time.Now().Add(time.Hour))
				require.ErrorIs(t, err, database.ErrConflict, "TTL must not apply the explicit deletion exception")
				// Timeout is not cleanup proof. Admission closes, but the task's
				// unpublished status remains hidden and the delete intent survives.
				actors.fail = true
				_, err = workflow.Delete(t.Context(), session)
				require.Error(t, err)
				current, err := store.GetSessionByID(t.Context(), session.Id)
				require.NoError(t, err)
				require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, current.State)
				if actors.readFailure {
					require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation, "preparation failure clears the operation but must not reopen settlement")
				}
				_, err = store.BeginIdleSessionDeletion(t.Context(), session.Id, time.Now().Add(time.Hour))
				require.ErrorIs(t, err, database.ErrConflict, "TTL must not take over a user's deletion intent")
				public, err := store.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.NoError(t, err)
				require.Equal(t, a2a.TaskStateSubmitted, public.Status.State)
				_, err = store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.ErrorIs(t, err, database.ErrConflict)
				require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), ""), database.ErrConflict)
				// Deletion also fences a cleanup acknowledgement that was delayed
				// in flight. It must not publish the boundary or enqueue a late push.
				require.ErrorIs(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), boundaryVersion), database.ErrConflict)
				public, err = store.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.NoError(t, err)
				require.Equal(t, a2a.TaskStateSubmitted, public.Status.State)
				require.Nil(t, public.Status.Message)
				delivery, err := store.ClaimDuePushDelivery(t.Context())
				require.NoError(t, err)
				require.Nil(t, delivery, "discarded boundary must not enqueue a callback")
				_, err = store.ClaimSessionQuiescence(t.Context())
				require.ErrorIs(t, err, database.ErrNotFound)
				// A runtime with stale credentials may still be alive after an
				// ambiguous delete. Neither a fresh task nor a new update is admitted.
				late := a2a.NewSubmittedTask(message, message)
				lateDigest := sha256.Sum256([]byte("late runtime write"))
				_, err = store.CreateRuntimeTask(t.Context(), session.Id, lateDigest[:], late, "")
				require.ErrorIs(t, err, database.ErrConflict)
				update := *task
				update.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
				_, err = store.UpdateSessionTask(t.Context(), session.Id, boundaryVersion, lateDigest[:], &update, &update, "")
				require.ErrorIs(t, err, database.ErrConflict)

				// A fresh controller reconciles both a failed deletion and a lost
				// response after the Actor was already deleted.
				actors.fail = false
				workflow = NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
				deleted, err := workflow.Delete(t.Context(), current)
				require.NoError(t, err)
				require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
				require.Empty(t, deleted.A2AAuthority)
				_, err = actors.GetActor(t.Context(), "team-a", name)
				require.Equal(t, codes.NotFound, status.Code(err))
				_, err = store.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.ErrorIs(t, err, database.ErrNotFound)
				_, err = store.GetSessionForRuntime(t.Context(), session.Id, actor.GetMetadata().GetUid())
				require.ErrorIs(t, err, database.ErrNotFound)
				require.ErrorIs(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), boundaryVersion), database.ErrNotFound)
				_, err = store.CreateRuntimeTask(t.Context(), session.Id, lateDigest[:], late, "")
				require.ErrorIs(t, err, database.ErrNotFound)
				again, err := workflow.Delete(t.Context(), current)
				require.NoError(t, err)
				require.Equal(t, deleted.State, again.State)
			})
		}
	}
}

type deletionFaultActors struct {
	*lifecycleTestActors
	fail              bool
	deleteBeforeError bool
	readFailure       bool
	suspendFailure    bool
}

func (a *deletionFaultActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if a.fail && a.readFailure {
		return nil, status.Error(codes.Unavailable, "injected preparation failure")
	}
	return a.lifecycleTestActors.GetActor(ctx, atespace, name)
}

func (a *deletionFaultActors) SuspendActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if a.fail && a.suspendFailure {
		if a.deleteBeforeError {
			if _, err := a.lifecycleTestActors.SuspendActor(ctx, atespace, name); err != nil {
				return nil, err
			}
		}
		return nil, status.Error(codes.Unavailable, "injected lost suspension response")
	}
	return a.lifecycleTestActors.SuspendActor(ctx, atespace, name)
}

func (a *deletionFaultActors) DeleteActor(ctx context.Context, atespace, name string) error {
	if a.fail {
		if a.deleteBeforeError {
			if err := a.lifecycleTestActors.DeleteActor(ctx, atespace, name); err != nil {
				return err
			}
		}
		return status.Error(codes.Unavailable, "injected lost deletion response")
	}
	return a.lifecycleTestActors.DeleteActor(ctx, atespace, name)
}
