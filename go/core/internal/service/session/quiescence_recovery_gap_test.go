package session

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// These assertions preserve the safety boundary, not a successful recovery.
// #3010's unpublished-boundary deletion escape does not cover a claimed idle
// operation. A replacement worker needs fencing and outcome reconciliation;
// dropping the claim because the old worker disappeared would be unsafe.
func TestDeleteRemainsBlockedByAbandonedQuiescence(t *testing.T) {
	for _, afterSuspend := range []bool{false, true} {
		name := "worker_lost_after_claim"
		if afterSuspend {
			name = "suspend_response_lost"
		}
		t.Run(name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			_, err = base.ResumeActor(t.Context(), "team-a", substrate.ActorName(session.Id))
			require.NoError(t, err)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("finished work"))
			message.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			task.Status.State = a2a.TaskStateCompleted
			digest := sha256.Sum256([]byte("completed"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
			require.NoError(t, err)
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
			claim, err := store.ClaimSessionQuiescence(t.Context())
			require.NoError(t, err)
			if afterSuspend {
				actors := &retryTestActors{lifecycleTestActors: base, mutationErr: status.Error(codes.Unavailable, "lost suspend response")}
				NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).quiesceIdleSession(t.Context(), claim)
				require.EqualValues(t, 1, actors.mutations.Load())
			}
			// Discard the owner. The replacement uses a new workflow and ordinary
			// startup scan methods, with the same durable store and healthy actors.
			restarted := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			_, err = store.ClaimSessionQuiescence(t.Context())
			require.ErrorIs(t, err, database.ErrNotFound)
			pending, err := store.HasPendingSessionQuiescence(t.Context())
			require.NoError(t, err)
			require.False(t, pending)
			public, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), ""), database.ErrDispatchBusy)
			_, err = restarted.Delete(t.Context(), session)
			require.ErrorIs(t, err, database.ErrFailedPrecondition)
			actor, readErr := base.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
			require.NoError(t, readErr)
			want := ateapipb.ActorState_ACTOR_STATE_RUNNING
			if afterSuspend {
				want = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
			}
			require.Equal(t, want, actor.GetStatus().GetState())
			t.Logf("public=%s; actor=%s; recovery scan has no candidate; DELETE=%v", public.Status.State, actor.GetStatus().GetState(), err)
		})
	}
}
