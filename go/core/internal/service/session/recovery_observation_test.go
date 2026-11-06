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
		name                                string
		running, crashed, replaced, missing bool
		readErr                             error
		want                                *bool
	}{
		{name: "healthy", want: new(false)},
		{name: "running", running: true, want: new(false)},
		{name: "crashed", crashed: true, want: new(true)},
		{name: "missing", missing: true},
		{name: "replaced", crashed: true, replaced: true},
		{name: "unavailable", readErr: status.Error(codes.Unavailable, "control plane unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			reverts := 0
			actors := &recoveryTestActors{lifecycleTestActors: base}
			actors.revert = func(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
				reverts++
				return base.RevertActor(ctx, atespace, name)
			}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			key := actorKey("team-a", substrate.ActorName(session.Id))
			if test.running {
				base.actors[key].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
			}
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
			before := proto.CloneOf(session)
			needed, err := workflow.RecoveryNeeded(t.Context(), session)
			if test.want == nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, needed)
			require.True(t, proto.Equal(before, session), "observing recovery must not mutate the Session")
			stored, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, stored.State, "reads do not admit recovery")

			// What chat offers is what Revert does: needed admits a revert, a
			// healthy runtime returns unchanged, and an unknown observation admits
			// nothing. A missing or replaced Actor is refused for good, so a client
			// stops retrying; only a failed read stays retryable.
			reverted, err := workflow.Revert(t.Context(), session)
			switch {
			case test.missing || test.replaced:
				require.ErrorIs(t, err, errRuntimeUnrecoverable)
			case test.want == nil:
				require.Error(t, err)
				require.NotErrorIs(t, err, database.ErrFailedPrecondition)
			case *test.want:
				require.NoError(t, err)
			default:
				require.NoError(t, err)
				require.True(t, proto.Equal(session, reverted), "a healthy Session is returned unchanged")
			}
			wantReverts := 0
			if test.want != nil && *test.want {
				wantReverts = 1
			}
			require.Equal(t, wantReverts, reverts, "only a confirmed crash is reverted")
		})
	}
}

func TestSessionObserveToleratesUnavailableRuntime(t *testing.T) {
	id := uuid.NewString()
	session := &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY}
	service := NewService(&serviceTestStore{getResult: session}, serviceTestAuthorizer{}, serviceTestWorkflow{
		recoveryErr: status.Error(codes.Unavailable, "control plane unavailable"),
	})
	got, err := service.Observe(serviceTestContext("alice"), id)
	require.NoError(t, err, "an unavailable runtime does not prevent history reads")
	require.Equal(t, id, got.Id)
	require.Nil(t, got.RecoveryNeeded, "an unavailable Actor must not be reported healthy")
}

// panicRecoveryWorkflow fails a test that observes Substrate outside GetSession.
type panicRecoveryWorkflow struct{ serviceTestWorkflow }

func (panicRecoveryWorkflow) RecoveryNeeded(context.Context, *apiv1.Session) (*bool, error) {
	panic("only GetSession observes recovery")
}

// Only GetSession observes Substrate. MCP resolves every invoke through Get, so
// observing there would add a control-plane round trip to each one.
func TestSessionRecoveryOnlyInGetSession(t *testing.T) {
	id := uuid.NewString()
	stored := &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY}
	ctx := serviceTestContext("alice")
	store := &serviceTestStore{getResult: stored, sessions: []*apiv1.Session{stored}}
	service := NewService(store, serviceTestAuthorizer{}, panicRecoveryWorkflow{})

	got, err := service.Get(ctx, id)
	require.NoError(t, err)
	require.Nil(t, got.RecoveryNeeded)
	listed, err := service.List(ctx, ListRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Sessions, 1)
	require.Nil(t, listed.Sessions[0].RecoveryNeeded)

	observed, err := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{recoveryNeeded: new(true)}).Observe(ctx, id)
	require.NoError(t, err)
	require.Equal(t, new(true), observed.RecoveryNeeded)
	require.Nil(t, stored.RecoveryNeeded, "observing does not mutate the stored record")
}
