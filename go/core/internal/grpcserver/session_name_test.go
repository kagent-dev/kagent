package grpcserver

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// runtimeNameStore holds one Session owned by "owner" and bound to one actor.
type runtimeNameStore struct {
	*database.Client
	mu      sync.Mutex
	session *apiv1.Session
}

func (s *runtimeNameStore) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	if id != s.session.Id || uid != "actor-uid" {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

func (s *runtimeNameStore) SetSessionNameIfEmpty(_ context.Context, id, owner, name string) (*apiv1.Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.session.Id || owner != s.session.Creator {
		return nil, false, database.ErrNotFound
	}
	if s.session.Name != "" {
		return s.session, false, nil
	}
	s.session.Name = name
	return s.session, true, nil
}

func TestRuntimeCallerNamesOnlyItsOwnSession(t *testing.T) {
	const ownID = "11111111-1111-4111-8111-111111111111"
	const otherID = "22222222-2222-4222-8222-222222222222"
	newClient := func(t *testing.T) (apiv1.SessionServiceClient, *runtimeNameStore, *jwttest.Issuer) {
		store := &runtimeNameStore{session: &apiv1.Session{
			Id: ownID, Creator: "owner",
			A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(ownID), ""),
			Agent:        &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}},
		}}
		issuer := jwttest.New(t)
		verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
		require.NoError(t, err)
		connection := newConfigurationConnection(t, nil, func(config *Config) {
			config.Authenticator = runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
			config.SessionService = sessionsvc.NewService(store, runtimeauth.NewAuthorizer(pkgauth.NoopAuthorizer{}, store), nil)
		})
		return apiv1.NewSessionServiceClient(connection), store, issuer
	}
	actor := func(t *testing.T, issuer *jwttest.Issuer) context.Context {
		token := issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(ownID), "actor-uid"))
		return metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token)
	}

	t.Run("Given an unnamed session, when its runtime sets a name, assert that the name is applied", func(t *testing.T) {
		client, store, issuer := newClient(t)
		response, err := client.SetSessionNameIfEmpty(actor(t, issuer), &apiv1.SetSessionNameIfEmptyRequest{SessionId: ownID, Name: "Generated title"})
		require.NoError(t, err)
		require.True(t, response.GetApplied())
		require.Equal(t, "Generated title", response.GetSession().GetName())
		require.Equal(t, "Generated title", store.session.GetName())
	})

	t.Run("Given a named session, when its runtime sets a name, assert that the existing name is kept", func(t *testing.T) {
		client, store, issuer := newClient(t)
		store.session.Name = "Chosen by a person"
		response, err := client.SetSessionNameIfEmpty(actor(t, issuer), &apiv1.SetSessionNameIfEmptyRequest{SessionId: ownID, Name: "Generated title"})
		require.NoError(t, err)
		require.False(t, response.GetApplied())
		require.Equal(t, "Chosen by a person", store.session.GetName())
	})

	t.Run("Given another session, when a runtime sets its name, assert that the call is denied", func(t *testing.T) {
		client, store, issuer := newClient(t)
		_, err := client.SetSessionNameIfEmpty(actor(t, issuer), &apiv1.SetSessionNameIfEmptyRequest{SessionId: otherID, Name: "Forged"})
		assertGRPCCode(t, err, codes.PermissionDenied)
		require.Empty(t, store.session.GetName())
	})

	t.Run("Given a session owned by someone else, when another user sets its name, assert that it is not found", func(t *testing.T) {
		client, store, _ := newClient(t)
		ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "mallory"))
		_, err := client.SetSessionNameIfEmpty(ctx, &apiv1.SetSessionNameIfEmptyRequest{SessionId: ownID, Name: "Forged"})
		assertGRPCCode(t, err, codes.NotFound)
		require.Empty(t, store.session.GetName())
	})
}
