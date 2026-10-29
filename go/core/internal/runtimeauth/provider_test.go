package runtimeauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
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

type bindingStore struct {
	session  *apiv1.Session
	actorUID string
}

var _ runtimeauth.SessionStore = (*bindingStore)(nil)

func (s *bindingStore) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	if s.session == nil || s.session.Id != id || uid != s.actorUID {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

func newBinding() *bindingStore {
	id := uuid.NewString()
	return &bindingStore{actorUID: "actor-uid", session: &apiv1.Session{
		Id: id, Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "agent"},
		A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), ""),
	}}
}

func TestSharedAuthenticationNeverFallsBackForActorCredentials(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	for _, claims := range []jwt.MapClaims{
		issuer.Claims("team-a", substrate.ActorName(store.session.Id), "replaced-uid"),
		issuer.Claims("other", substrate.ActorName(store.session.Id), store.actorUID),
		issuer.Claims("team-a", substrate.ActorName(uuid.NewString()), store.actorUID),
		{"iss": issuer.URL, "sub": "forged-user"},
		{"iss": "other", "actor.ate.dev": map[string]any{}},
	} {
		headers := http.Header{"Authorization": {"Bearer " + issuer.Token(t, claims)}, "X-User-Id": {"admin"}}
		_, err := provider.Authenticate(t.Context(), headers, nil)
		require.Error(t, err)
	}
	user, err := provider.Authenticate(t.Context(), http.Header{"X-User-Id": {"alice"}, "X-Agent-Name": {"team-a/agent"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", user.Principal().ID)
	require.Nil(t, user.Principal().RuntimeBinding)
}

func TestSharedHTTPAuthenticationBindsActorsAndPreservesUserMode(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	store := newBinding()
	token := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))
	for _, mode := range []struct {
		name      string
		users     auth.AuthProvider
		anonymous int
	}{
		{"insecure", &authimpl.InsecureAuthenticator{}, http.StatusOK},
		{"proxy", authimpl.NewProxyAuthenticator("sub"), http.StatusUnauthorized},
	} {
		t.Run(mode.name, func(t *testing.T) {
			provider := runtimeauth.NewProvider(mode.users, verifier, store, issuer.URL)
			handler := auth.AuthnMiddleware(provider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session, ok := auth.AuthSessionFrom(r.Context())
				require.True(t, ok)
				if r.Header.Get("Authorization") == token {
					require.Equal(t, auth.Principal{ID: auth.PrincipalID("substrate-actor", "team-a/actor-uid"), RuntimeBinding: &auth.RuntimeBinding{SessionID: store.session.Id, CreatorID: "owner"}}, session.Principal())
				}
				w.WriteHeader(http.StatusOK)
			}))
			for _, test := range []struct {
				token, share string
				status       int
			}{
				{token, "", http.StatusOK}, {token, "share", http.StatusUnauthorized}, {"", "", mode.anonymous},
			} {
				request := httptest.NewRequest(http.MethodPost, "/mcp?user_id=forged", nil)
				request.Header.Set("Authorization", test.token)
				request.Header.Set("X-User-Id", "forged")
				request.Header.Set("X-Agent-Name", "other/agent")
				request.Header.Set("X-Share-Token", test.share)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.Equal(t, test.status, response.Code)
			}
		})
	}
}

func TestActorUpstreamIdentityIsBoundAndDoesNotForwardBearer(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))}}, nil)
	require.NoError(t, err)
	target := store.session.Id
	request := httptest.NewRequest(http.MethodPost, "http://runtime", nil)
	request.Header.Set("Authorization", "Bearer untrusted")
	require.NoError(t, provider.UpstreamAuth(request, identity, target))
	require.Empty(t, request.Header.Get("Authorization"))
	require.Equal(t, "owner", request.Header.Get("X-User-Id"))
	require.Error(t, provider.UpstreamAuth(request, identity, ""), "an Actor cannot dispatch to an unbound upstream")
	require.Error(t, provider.UpstreamAuth(request, identity, uuid.NewString()))
	store.actorUID = "replacement"
	require.Error(t, provider.UpstreamAuth(request, identity, target))
}
