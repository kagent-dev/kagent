package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestProjectedSessionRotationAndIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const audience = "kagent-invocation"
		const outboundAudience = "mcp.example"
		token := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller"})
		outboundToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience}})
		rotated := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience}, "generation": 2})
		dualAudience := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience, audience}})
		apiAccepted := false
		apiToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience}, "generation": 3})
		uid, authenticated, acceptedAudience := "sa-uid", true, audience
		var reviewErr error
		client := fake.NewClientset()
		client.PrependReactor("create", "tokenreviews", func(action kubetesting.Action) (bool, runtime.Object, error) {
			review := action.(kubetesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
			if len(review.Spec.Audiences) == 0 {
				return true, &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{Authenticated: apiAccepted}}, reviewErr
			}
			wantAudience := audience
			if review.Spec.Token != token {
				wantAudience = outboundAudience
			}
			actualAudience := acceptedAudience
			if actualAudience == audience {
				actualAudience = wantAudience
			}
			assert.Contains(t, []string{token, outboundToken, rotated, dualAudience, apiToken}, review.Spec.Token)
			if len(review.Spec.Audiences) != 1 || review.Spec.Audiences[0] != wantAudience {
				return true, &authenticationv1.TokenReview{}, nil
			}
			return true, &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{
				Authenticated: authenticated, Audiences: []string{actualAudience},
				User: authenticationv1.UserInfo{Username: "system:serviceaccount:kagent:controller", UID: uid, Groups: []string{"system:serviceaccounts"}},
			}}, reviewErr
		})
		p := authimpl.NewServiceAccountProvider(&authimpl.InsecureAuthenticator{}, client.AuthenticationV1().TokenReviews(), "cluster-a")
		headers := http.Header{"Authorization": {"Bearer " + token}, "X-User-Id": {"forged"}, "X-Agent-Name": {"forged/agent"}}
		session, err := p.Authenticate(t.Context(), headers, nil)
		require.NoError(t, err)
		wantID := auth.PrincipalID("kubernetes:cluster-a", uid)
		require.Equal(t, wantID, session.Principal().ID)
		require.Nil(t, session.Principal().RuntimeBinding)
		request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
		require.NoError(t, p.UpstreamAuth(request, session, ""))
		require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))

		path := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(path, []byte(token), 0o600))
		outboundPath := filepath.Join(t.TempDir(), "outbound-token")
		require.NoError(t, os.WriteFile(outboundPath, []byte(outboundToken), 0o600))
		scheduled, err := p.ProjectedSession(t.Context(), path, outboundPath, outboundAudience)
		require.NoError(t, err)
		require.Equal(t, wantID, scheduled.Principal().ID)
		for range 2 {
			require.NoError(t, p.UpstreamAuth(request, scheduled, ""))
			require.Equal(t, "Bearer "+outboundToken, request.Header.Get("Authorization"))
		}
		require.Len(t, client.Actions(), 3, "inbound and outbound verification is cached independently")
		require.NoError(t, os.WriteFile(outboundPath, []byte(rotated), 0o600))
		require.NoError(t, p.UpstreamAuth(request, scheduled, ""))
		require.Equal(t, "Bearer "+rotated, request.Header.Get("Authorization"))
		require.Len(t, client.Actions(), 5, "rotation must verify the new token immediately")
		for _, caller := range []auth.Session{session, scheduled} {
			hookError := errors.New("upstream credentials suppressed")
			provider := &upstreamAuthProvider{AuthProvider: &authimpl.InsecureAuthenticator{}, upstreamAuth: func(req *http.Request, got auth.Session, destination string) error {
				require.Equal(t, caller, got)
				require.Equal(t, "destination-session", destination)
				req.Header.Del("Authorization")
				return hookError
			}}
			wrapped := authimpl.NewServiceAccountProvider(provider, client.AuthenticationV1().TokenReviews(), "cluster-a")
			outbound := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
			require.ErrorIs(t, wrapped.UpstreamAuth(outbound, caller, "destination-session"), hookError)
			require.Empty(t, outbound.Header.Get("Authorization"))
		}
		_, err = p.Authenticate(t.Context(), request.Header, nil)
		require.Error(t, err, "outbound credential must not authenticate to Kagent or fall back to insecure auth")
		_, err = p.ProjectedSession(t.Context(), path, outboundPath, audience)
		require.Error(t, err, "inbound and outbound audiences must differ")
		require.NoError(t, os.WriteFile(outboundPath, []byte(dualAudience), 0o600))
		_, err = p.ProjectedSession(t.Context(), path, outboundPath, outboundAudience)
		require.ErrorContains(t, err, "outbound credential grants inbound Kagent access")
		require.Error(t, p.UpstreamAuth(request, scheduled, ""), "rotation must not introduce inbound authority")
		require.Empty(t, request.Header.Get("Authorization"))
		require.NoError(t, os.WriteFile(outboundPath, nil, 0o600))
		_, err = p.ProjectedSession(t.Context(), path, outboundPath, outboundAudience)
		require.ErrorContains(t, err, "projected invocation credential unavailable")
		require.NoError(t, os.WriteFile(outboundPath, []byte(rotated), 0o600))

		// A newly projected token must not introduce Kubernetes API authority.
		apiAccepted = true
		require.NoError(t, os.WriteFile(outboundPath, []byte(apiToken), 0o600))
		require.ErrorContains(t, p.UpstreamAuth(request, scheduled, ""), "outbound credential grants Kubernetes API access")
		_, err = authimpl.NewServiceAccountProvider(&authimpl.InsecureAuthenticator{}, client.AuthenticationV1().TokenReviews(), "cluster-a").ProjectedSession(t.Context(), path, outboundPath, outboundAudience)
		require.ErrorContains(t, err, "outbound credential grants Kubernetes API access")
		apiAccepted = false
		require.NoError(t, os.WriteFile(outboundPath, []byte(rotated), 0o600))

		for _, failure := range []string{"revoked", "audience", "unavailable", "recreated"} {
			time.Sleep(30 * time.Second) // Refresh verification after the revocation window.
			authenticated, acceptedAudience, uid, reviewErr = true, audience, "sa-uid", nil
			switch failure {
			case "revoked":
				authenticated = false
			case "audience":
				acceptedAudience = "kubernetes-api"
			case "unavailable":
				reviewErr = errors.New("sensitive token " + token)
			case "recreated":
				uid = "replacement-sa"
			}
			request.Header.Set("Authorization", "Bearer previous-caller")
			err := p.UpstreamAuth(request, scheduled, "")
			require.Error(t, err, failure)
			require.NotContains(t, err.Error(), token)
			require.Empty(t, request.Header.Get("Authorization"))
			if failure != "recreated" {
				_, err = p.Authenticate(t.Context(), headers, nil)
				require.Error(t, err, "must not fall back to insecure authentication")
			}
		}
		client.ClearActions()
		headers.Add("Authorization", "Bearer duplicate")
		_, err = p.Authenticate(t.Context(), headers, nil)
		require.Error(t, err)
		require.Empty(t, client.Actions())
	})
}
