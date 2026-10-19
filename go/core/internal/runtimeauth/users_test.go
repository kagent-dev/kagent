package runtimeauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestActorCredentialsNeverAuthenticateAsUsers(t *testing.T) {
	issuer := jwttest.New(t)
	provider := runtimeauth.Users(&authimpl.InsecureAuthenticator{}, issuer.URL)
	for _, claims := range []jwt.MapClaims{
		issuer.Claims("team-a", "session-"+uuid.NewString(), "actor-uid"),
		{"iss": issuer.URL, "sub": "forged-user"},
		{"iss": "other", "actor.ate.dev": map[string]any{}},
	} {
		headers := http.Header{"Authorization": {"Bearer " + issuer.Token(t, claims)}, "X-User-Id": {"admin"}}
		_, err := provider.Authenticate(t.Context(), headers, nil)
		require.Error(t, err)
	}
	user, err := provider.Authenticate(t.Context(), http.Header{"X-User-Id": {"alice"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", user.Principal().User.ID, "configured user auth remains in effect")
}

func TestRuntimeHTTPRequiresActorCredentials(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	handler := auth.AuthnMiddleware(runtimeauth.NewAuthenticator(verifier))(http.NotFoundHandler())
	token := "Bearer " + issuer.Token(t, issuer.Claims("team-a", "session-"+uuid.NewString(), "actor-uid"))
	for _, path := range []string{"/mcp", "/agents/team-a/agent", "/health"} {
		for _, test := range []struct {
			token  string
			status int
		}{
			{"", http.StatusUnauthorized}, {"Bearer placeholder", http.StatusUnauthorized}, {token, http.StatusNotFound},
		} {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", test.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code)
		}
	}
}
