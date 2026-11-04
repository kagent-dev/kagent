package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestSessionRecoveryObservation(t *testing.T) {
	for _, test := range []struct {
		name                       string
		crashed, replaced, missing bool
		readErr                    error
		want                       *bool
	}{
		{name: "healthy", want: new(false)},
		{name: "crashed", crashed: true, want: new(true)},
		{name: "missing", missing: true},
		{name: "replaced", crashed: true, replaced: true},
		{name: "unavailable", readErr: status.Error(codes.Unavailable, "control plane unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			actors := &recoveryTestActors{lifecycleTestActors: base, revert: base.RevertActor}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			key := actorKey("team-a", substrate.ActorName(session.Id))
			if test.crashed {
				base.actors[key].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
			}
			if test.replaced {
				base.actors[key].Metadata.Uid = "replacement"
			}
			if test.missing {
				delete(base.actors, key)
			}
			actors.readErr = test.readErr
			session.Recovery = &apiv1.SessionRecovery{FailedMessages: []*apiv1.SessionMessageReference{{TaskId: "task", MessageId: "message"}}}
			before := proto.CloneOf(session)
			observed, err := workflow.Recovery(t.Context(), session)
			if test.want == nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, observed.Needed)
			require.Equal(t, session.Recovery.FailedMessages, observed.FailedMessages)
			require.True(t, proto.Equal(before, session), "observing recovery must not mutate the Session or its history")
			stored, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, stored.State)
			require.Nil(t, stored.Recovery, "reads do not admit recovery or persist observations")

			// What chat offers is what Revert does: needed admits a revert,
			// healthy refuses one, and an unknown observation admits nothing.
			// A missing or replaced Actor is refused for good, so a client stops
			// retrying; only a failed read stays retryable.
			_, err = workflow.Revert(t.Context(), session)
			switch {
			case test.missing || test.replaced:
				require.ErrorIs(t, err, errRuntimeUnrecoverable)
			case test.want == nil:
				require.Error(t, err)
				require.NotErrorIs(t, err, database.ErrFailedPrecondition)
			case *test.want:
				require.NoError(t, err)
			default:
				require.ErrorIs(t, err, errRuntimeNotCrashed)
			}
		})
	}
}

func TestSessionObservePreservesHistoryWhenRecoveryObservationFails(t *testing.T) {
	id := uuid.NewString()
	session := &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY}
	observation := &apiv1.SessionRecovery{FailedMessages: []*apiv1.SessionMessageReference{{TaskId: "task", MessageId: "message"}}}
	service := NewService(&serviceTestStore{getResult: session}, serviceTestAuthorizer{}, serviceTestWorkflow{
		recovery: observation, recoveryErr: status.Error(codes.Unavailable, "control plane unavailable"),
	})
	got, err := service.Observe(serviceTestContext("alice"), id)
	require.NoError(t, err)
	require.Equal(t, id, got.Id)
	require.Equal(t, observation.FailedMessages, got.Recovery.FailedMessages)
	require.Nil(t, got.Recovery.Needed, "an unavailable Actor must not be reported healthy")
	require.Nil(t, session.Recovery, "the stored record remains unchanged")
}

// panicRecoveryWorkflow fails a test that observes Substrate outside GetSession.
type panicRecoveryWorkflow struct{ serviceTestWorkflow }

func (panicRecoveryWorkflow) Recovery(context.Context, *apiv1.Session) (*apiv1.SessionRecovery, error) {
	panic("only GetSession observes recovery")
}

// Only GetSession observes Substrate or returns recovery. MCP resolves every
// invoke through Get, and the annotations grow with each lossy revert, so any
// other response carrying them would cost a control-plane round trip or ship
// every conversation's annotations in a list.
func TestSessionRecoveryOnlyInGetSession(t *testing.T) {
	id := uuid.NewString()
	annotations := []*apiv1.SessionMessageReference{{TaskId: "task", MessageId: "message"}}
	stored := func() *apiv1.Session {
		return &apiv1.Session{
			Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
			Recovery: &apiv1.SessionRecovery{FailedMessages: annotations},
		}
	}
	ctx := serviceTestContext("alice")
	store := &serviceTestStore{getResult: stored(), sessions: []*apiv1.Session{stored()}}
	service := NewService(store, serviceTestAuthorizer{}, panicRecoveryWorkflow{})

	got, err := service.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, annotations, got.GetRecovery().GetFailedMessages(), "Get serves the stored record unchanged")

	listed, err := service.List(ctx, ListRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Sessions, 1)
	require.Nil(t, listed.Sessions[0].Recovery)
	require.NotNil(t, store.sessions[0].Recovery, "omitting recovery does not mutate the stored record")

	reverted, err := service.Revert(ctx, id)
	require.NoError(t, err)
	require.Nil(t, reverted.Recovery)
	deleted, err := service.Delete(ctx, id)
	require.NoError(t, err)
	require.Nil(t, deleted.Recovery)

	// Observing reports the workflow's result without changing the stored record.
	store.getResult = stored()
	store.getResult.Recovery.Needed = new(true)
	observation := &apiv1.SessionRecovery{FailedMessages: annotations, Needed: new(false)}
	observed, err := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{recovery: observation}).Observe(ctx, id)
	require.NoError(t, err)
	require.Equal(t, new(false), observed.Recovery.Needed)
	require.Equal(t, annotations, observed.Recovery.FailedMessages)
	require.Equal(t, new(true), store.getResult.Recovery.Needed, "observing does not mutate the stored record")
}
