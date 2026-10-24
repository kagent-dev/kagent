package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestServiceAccountVerificationAndRotation(t *testing.T) {
	const audience = "kagent-invocation"
	const outboundAudience = "mcp.example"
	token := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller"})
	outboundToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience}})
	rotated := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience}, "generation": 2})
	dualAudience := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "aud": []string{outboundAudience, audience}})
	uid, authenticated, acceptedAudience := "sa-uid", true, audience
	var reviewErr error
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(action kubetesting.Action) (bool, runtime.Object, error) {
		review := action.(kubetesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		wantAudience := audience
		if review.Spec.Token != token {
			wantAudience = outboundAudience
		}
		actualAudience := acceptedAudience
		if actualAudience == audience {
			actualAudience = wantAudience
		}
		require.Contains(t, []string{token, outboundToken, rotated, dualAudience}, review.Spec.Token)
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
	require.NoError(t, os.WriteFile(outboundPath, []byte(rotated), 0o600))
	require.NoError(t, p.UpstreamAuth(request, scheduled, ""))
	require.Equal(t, "Bearer "+rotated, request.Header.Get("Authorization"))
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

	for _, failure := range []string{"expired", "audience", "unavailable", "recreated"} {
		t.Run(failure, func(t *testing.T) {
			authenticated, acceptedAudience, uid, reviewErr = true, audience, "sa-uid", nil
			switch failure {
			case "expired":
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
			require.Error(t, err)
			require.NotContains(t, err.Error(), token)
			require.Empty(t, request.Header.Get("Authorization"))
			if failure != "recreated" {
				_, err = p.Authenticate(t.Context(), headers, nil)
				require.Error(t, err, "must not fall back to insecure authentication")
			}
		})
	}
	client.ClearActions()
	headers.Add("Authorization", "Bearer duplicate")
	_, err = p.Authenticate(t.Context(), headers, nil)
	require.Error(t, err)
	require.Empty(t, client.Actions())
}

type upstreamAuthProvider struct {
	auth.AuthProvider
	upstreamAuth func(*http.Request, auth.Session, string) error
}

var _ auth.AuthProvider = (*upstreamAuthProvider)(nil)

func (p *upstreamAuthProvider) UpstreamAuth(request *http.Request, session auth.Session, destination string) error {
	return p.upstreamAuth(request, session, destination)
}

func TestPrincipalAuthoritiesDoNotCollide(t *testing.T) {
	p := authimpl.NewProxyAuthenticator("")
	ids := map[string]bool{}
	for _, issuer := range []string{"", "issuer-a", "issuer-b"} {
		token := createTestJWT(map[string]any{"iss": issuer, "sub": "same-subject"})
		session, err := p.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + token}}, nil)
		require.NoError(t, err)
		require.False(t, ids[session.Principal().ID])
		ids[session.Principal().ID] = true
	}
	require.False(t, ids[auth.PrincipalID("kubernetes:issuer-a", "same-subject")])
	require.NotEqual(t, auth.PrincipalID("a/b", "c"), auth.PrincipalID("a", "b/c"))
}
