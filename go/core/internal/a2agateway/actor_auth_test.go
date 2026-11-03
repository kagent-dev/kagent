package a2agateway

import (
	"context"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type actorBinding struct{ session *apiv1.Session }

func (s actorBinding) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	if id != s.session.Id || uid != "actor-uid" {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

func TestHTTPActorA2AUsesSessionAuthorization(t *testing.T) {
	session := gatewayTestSession()
	session.A2AAuthority = substrate.ActorHost("team-a", substrate.ActorName(session.Id), "")
	binding := actorBinding{session: session}
	store := &gatewayTestStore{session: session, task: &a2atype.Task{ID: "runtime-task", ContextID: session.Id, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, binding, issuer.URL)
	policy := runtimeauth.NewAuthorizer(auth.NoopAuthorizer{}, binding)
	runtime := &gatewayTestRuntime{task: store.task}
	gateway := newTestGateway(store, policy, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)
	server := httptest.NewServer(NewHTTPHandler(gateway, provider, nil))
	t.Cleanup(server.Close)
	transport := newGatewayTestTransport(t, server.Listener.Addr().String(), a2atype.TransportProtocolJSONRPC)
	token := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid"))
	actor := a2aclient.ServiceParams{"authorization": {token}, "x-user-id": {"forged"}}
	user := a2aclient.ServiceParams{"x-user-id": {"alice"}}
	for _, params := range []a2aclient.ServiceParams{actor, user} {
		task, err := transport.GetTask(t.Context(), params, &a2atype.GetTaskRequest{ID: "runtime-task"})
		require.NoError(t, err)
		require.Equal(t, store.task.ID, task.ID)
		require.Equal(t, "alice", store.userID)
	}
	_, err = transport.SendMessage(t.Context(), actor, gatewayTestRequest())
	require.NoError(t, err)
	require.True(t, runtime.sent, "an authorized actor call reaches its runtime")
	_, err = transport.GetTask(t.Context(), a2aclient.ServiceParams{"authorization": {token}, "x-share-token": {"share"}}, &a2atype.GetTaskRequest{ID: "runtime-task"})
	require.Error(t, err)
	// A different session for the same owner is still outside actor authority.
	other := gatewayTestSession()
	other.Id = uuid.NewString()
	store.session = other
	_, err = transport.GetTask(t.Context(), actor, &a2atype.GetTaskRequest{ID: "runtime-task"})
	require.ErrorIs(t, err, a2atype.ErrUnauthorized)
	_, err = transport.GetTask(t.Context(), user, &a2atype.GetTaskRequest{ID: "runtime-task"})
	require.NoError(t, err)
}
