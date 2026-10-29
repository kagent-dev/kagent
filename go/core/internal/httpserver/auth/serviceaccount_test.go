package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestServiceAccountVerification(t *testing.T) {
	const audience = "kagent-invocation"
	token := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:controller", "exp": time.Now().Add(time.Hour).Unix()})
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(action kubetesting.Action) (bool, runtime.Object, error) {
		review := action.(kubetesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		assert.Equal(t, token, review.Spec.Token)
		assert.Equal(t, []string{audience}, review.Spec.Audiences)
		return true, &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{
			Authenticated: true, Audiences: []string{audience},
			User: authenticationv1.UserInfo{Username: "system:serviceaccount:kagent:controller", UID: "sa-uid", Groups: []string{"system:serviceaccounts"}},
		}}, nil
	})
	p := authimpl.NewServiceAccountProvider(&authimpl.InsecureAuthenticator{}, client.AuthenticationV1().TokenReviews(), "cluster-a")
	headers := http.Header{"Authorization": {"Bearer " + token}, "X-User-Id": {"forged"}, "X-Agent-Name": {"forged/agent"}}
	session, err := p.Authenticate(t.Context(), headers, nil)
	require.NoError(t, err)
	require.Equal(t, auth.PrincipalID("kubernetes:cluster-a", "sa-uid"), session.Principal().ID)
	require.Nil(t, session.Principal().RuntimeBinding)
	for range 2 {
		request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
		require.NoError(t, p.UpstreamAuth(request, session, ""))
		require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
	}
	require.Len(t, client.Actions(), 1, "forwarding must reuse the request's verified token")
	_, err = p.Authenticate(t.Context(), headers, nil)
	require.NoError(t, err)
	require.Len(t, client.Actions(), 1, "a new request may reuse the unexpired verification")

	provider := &upstreamAuthProvider{AuthProvider: &authimpl.InsecureAuthenticator{}, upstreamAuth: func(req *http.Request, got auth.Session, destination string) error {
		require.Equal(t, session, got)
		require.Equal(t, "destination-session", destination)
		req.Header.Del("Authorization")
		return nil
	}}
	wrapped := authimpl.NewServiceAccountProvider(provider, client.AuthenticationV1().TokenReviews(), "cluster-a")
	request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
	require.NoError(t, wrapped.UpstreamAuth(request, session, "destination-session"))
	require.Empty(t, request.Header.Get("Authorization"))

	client.ClearActions()
	headers.Add("Authorization", "Bearer duplicate")
	_, err = p.Authenticate(t.Context(), headers, nil)
	require.Error(t, err)
	require.Empty(t, client.Actions())
}

func TestServiceAccountVerificationFailsClosed(t *testing.T) {
	for _, failure := range []string{"unauthenticated", "audience", "username", "uid", "unavailable"} {
		t.Run(failure, func(t *testing.T) {
			client := fake.NewClientset()
			client.PrependReactor("create", "tokenreviews", func(kubetesting.Action) (bool, runtime.Object, error) {
				status := authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{"kagent-invocation"}, User: authenticationv1.UserInfo{Username: "system:serviceaccount:ns:caller", UID: "uid"}}
				switch failure {
				case "unauthenticated":
					status.Authenticated = false
				case "audience":
					status.Audiences = []string{"kubernetes-api"}
				case "username":
					status.User.Username = "user"
				case "uid":
					status.User.UID = ""
				case "unavailable":
					return true, nil, errors.New("sensitive response")
				}
				return true, &authenticationv1.TokenReview{Status: status}, nil
			})
			p := authimpl.NewServiceAccountProvider(&authimpl.InsecureAuthenticator{}, client.AuthenticationV1().TokenReviews(), "cluster")
			for _, claims := range []map[string]any{{"sub": "system:serviceaccount:ns:caller"}, {"sub": "federated-user", "kubernetes.io": map[string]any{}}} {
				session, err := p.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + createTestJWT(claims)}}, nil)
				require.Error(t, err, "Kubernetes-shaped credentials must not fall back to the user provider")
				require.NotContains(t, err.Error(), "sensitive response")
				require.Nil(t, session)
			}
		})
	}
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
