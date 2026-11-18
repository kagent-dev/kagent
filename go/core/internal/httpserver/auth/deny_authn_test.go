package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestDenyAuthenticatorBehindServiceAccountProvider(t *testing.T) {
	saToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:apps:worker", "exp": time.Now().Add(time.Hour).Unix()})
	client := fake.NewClientset()
	client.PrependReactor("create", "tokenreviews", func(kubetesting.Action) (bool, runtime.Object, error) {
		return true, &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{
			Authenticated: true, Audiences: []string{"kagent-invocation"},
			User: authenticationv1.UserInfo{Username: "system:serviceaccount:apps:worker", UID: "worker-uid"},
		}}, nil
	})
	provider := authimpl.NewServiceAccountProvider(authimpl.DenyAuthenticator{}, client.AuthenticationV1().TokenReviews(), "kubernetes")

	t.Run("Given deny mode with service accounts, when a verified service-account token calls, assert that it authenticates and is forwarded", func(t *testing.T) {
		session, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + saToken}, "X-User-Id": {"admin@kagent.dev"}}, nil)

		require.NoError(t, err)
		require.Equal(t, auth.PrincipalID("kubernetes:kubernetes", "worker-uid"), session.Principal().ID)
		request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
		require.NoError(t, provider.UpstreamAuth(request, session, ""))
		require.Equal(t, "Bearer "+saToken, request.Header.Get("Authorization"))
		require.Equal(t, session.Principal().ID, request.Header.Get("X-User-Id"))
	})

	for _, tt := range []struct {
		name    string
		headers http.Header
		query   url.Values
	}{
		{name: "no credentials"},
		{name: "a forged X-User-Id header", headers: http.Header{"X-User-Id": {"admin@kagent.dev"}}},
		{name: "a user_id query parameter", query: url.Values{"user_id": {"admin@kagent.dev"}}},
		{name: "an unverified user JWT", headers: http.Header{"Authorization": {"Bearer " + createTestJWT(map[string]any{"sub": "user", "email": "user@example.com"})}}},
	} {
		t.Run("Given deny mode with service accounts, when a caller sends "+tt.name+", assert that it is unauthenticated without a TokenReview", func(t *testing.T) {
			client.ClearActions()
			headers := tt.headers
			if headers == nil {
				headers = http.Header{}
			}

			session, err := provider.Authenticate(t.Context(), headers, tt.query)

			require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
			require.Nil(t, session)
			require.Empty(t, client.Actions())
		})
	}
}
