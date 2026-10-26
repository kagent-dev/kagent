package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
)

func TestProjectedCredentialCacheExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reviewer := &tokenReviewer{create: func(_ context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
			if len(req.Spec.Audiences) == 0 {
				return &authenticationv1.TokenReview{}, nil
			}
			return acceptedReview(req), nil
		}}
		p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
		identity := filepath.Join(t.TempDir(), "identity")
		outbound := filepath.Join(t.TempDir(), "outbound")
		require.NoError(t, os.WriteFile(identity, []byte(reviewToken(t, jwt.MapClaims{"aud": serviceAccountAudience})), 0o600))
		token := reviewToken(t, jwt.MapClaims{"aud": "mcp.example", "exp": time.Now().Add(5 * time.Second).Unix()})
		require.NoError(t, os.WriteFile(outbound, []byte(token), 0o600))
		session, err := p.ProjectedSession(t.Context(), identity, outbound, "mcp.example")
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
		require.NoError(t, p.UpstreamAuth(request, session, ""))
		require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
		require.EqualValues(t, 3, reviewer.calls.Load())
		time.Sleep(5 * time.Second)
		require.ErrorIs(t, p.UpstreamAuth(request, session, ""), ErrUnauthenticated)
		require.Empty(t, request.Header.Get("Authorization"), "cached verification must not forward an expired token")
		rotated := reviewToken(t, jwt.MapClaims{"aud": "mcp.example", "exp": time.Now().Add(time.Hour).Unix()})
		require.NoError(t, os.WriteFile(outbound, []byte(rotated), 0o600))
		require.NoError(t, p.UpstreamAuth(request, session, ""))
		require.Equal(t, "Bearer "+rotated, request.Header.Get("Authorization"))
		require.EqualValues(t, 5, reviewer.calls.Load())
	})
}
