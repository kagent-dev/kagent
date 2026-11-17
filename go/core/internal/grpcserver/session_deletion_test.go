package grpcserver

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric/noop"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestSessionDeletionForceThroughGeneratedClient(t *testing.T) {
	connectionString := dbtest.StartT(t.Context(), t)
	dbtest.MigrateT(t, connectionString, false)
	pool, err := pgxpool.New(t.Context(), connectionString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	session := createTaskStoreSession(t, store)
	input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("keep working"))
	input.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(input, input)
	digest := sha256.Sum256([]byte("active turn"))
	_, err = store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
	require.NoError(t, err)
	actors := &sessionDeleteActors{actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: substrate.ActorName(session.Id), Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "assistant-kagent-revision"},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_RUNNING},
	}}
	workflow := sessionsvc.NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.SessionService = sessionsvc.NewService(store, sessionAuthorizationPolicy{}, workflow)
	})
	client := apiv1.NewSessionServiceClient(connection)
	alice := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
	_, err = client.DeleteSession(alice, &apiv1.DeleteSessionRequest{SessionId: session.Id})
	assertGRPCCode(t, err, codes.Aborted)
	current, err := client.GetSession(alice, &apiv1.GetSessionRequest{SessionId: session.Id})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	bob := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "bob"))
	_, err = client.DeleteSession(bob, &apiv1.DeleteSessionRequest{SessionId: session.Id, Force: true})
	assertGRPCCode(t, err, codes.NotFound)
	deleted, err := client.DeleteSession(alice, &apiv1.DeleteSessionRequest{SessionId: session.Id, Force: true})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.GetSession().GetState())
	_, err = actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.GetSession(alice, &apiv1.GetSessionRequest{SessionId: session.Id})
	assertGRPCCode(t, err, codes.NotFound)
}

// Every runtime method except lookup and deletion is forbidden in this fixture.
type sessionDeleteActors struct {
	substrate.LifecycleClient
	mu    sync.Mutex
	actor *ateapipb.Actor
}

func (a *sessionDeleteActors) GetActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.actor == nil {
		return nil, status.Error(codes.NotFound, "deleted")
	}
	return proto.CloneOf(a.actor), nil
}

func (a *sessionDeleteActors) DeleteActor(_ context.Context, atespace, name, uid string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.actor.GetMetadata().GetUid() != uid || a.actor.GetMetadata().GetAtespace() != atespace || a.actor.GetMetadata().GetName() != name {
		return status.Error(codes.Aborted, "actor identity changed")
	}
	a.actor = nil
	return nil
}
