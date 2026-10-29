package session

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
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
			actors := &recoveryTestActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, false)
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
		})
	}
}

func TestSessionGetPreservesHistoryWhenRecoveryObservationFails(t *testing.T) {
	id := uuid.NewString()
	session := &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY}
	observation := &apiv1.SessionRecovery{FailedMessages: []*apiv1.SessionMessageReference{{TaskId: "task", MessageId: "message"}}}
	service := NewService(&serviceTestStore{getResult: session}, serviceTestAuthorizer{}, serviceTestWorkflow{
		recovery: observation, recoveryErr: status.Error(codes.Unavailable, "control plane unavailable"),
	})
	got, err := service.Get(serviceTestContext("alice"), id)
	require.NoError(t, err)
	require.Equal(t, id, got.Id)
	require.Equal(t, observation.FailedMessages, got.Recovery.FailedMessages)
	require.Nil(t, got.Recovery.Needed, "an unavailable Actor must not be reported healthy")
	require.Nil(t, session.Recovery, "the stored record remains unchanged")
}
