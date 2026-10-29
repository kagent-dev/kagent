package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedauth "k8s.io/client-go/kubernetes/typed/authentication/v1"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestSchedulerCredentialRotationAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "outbound-token")
		token := createTestJWT(map[string]any{"aud": "mcp.example", "exp": time.Now().Add(5 * time.Second).Unix()})
		require.NoError(t, os.WriteFile(path, []byte(token+"\n"), 0o600))
		// No verifier or inbound token is needed. The projected file is trusted;
		// its claims and signature do not establish the scheduler's identity.
		session, err := authimpl.NewSchedulerSession(t.Context(), &apiReview{}, "kagent", path, "mcp.example")
		require.NoError(t, err)
		principal := session.Principal()
		require.NotEmpty(t, principal.ID)
		require.Nil(t, principal.RuntimeBinding)
		require.Empty(t, principal.Claims)
		request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
		require.NoError(t, auth.ForwardCredential(request, session))
		require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
		require.Equal(t, principal.ID, request.Header.Get("X-User-Id"))

		time.Sleep(5 * time.Second)
		require.ErrorIs(t, auth.ForwardCredential(request, session), authimpl.ErrUnauthenticated)
		require.Empty(t, request.Header.Get("Authorization"))

		rotated := createTestJWT(map[string]any{
			"aud": "mcp.example", "exp": time.Now().Add(time.Hour).Unix(),
			"sub": "system:serviceaccount:kagent:replacement", "iss": "different-issuer",
		})
		require.NoError(t, os.WriteFile(path, []byte(rotated), 0o600))
		require.NoError(t, auth.ForwardCredential(request, session))
		require.Equal(t, "Bearer "+rotated, request.Header.Get("Authorization"))
		require.Equal(t, principal, session.Principal())
		restarted, err := authimpl.NewSchedulerSession(t.Context(), &apiReview{}, "kagent", path, "mcp.example")
		require.NoError(t, err)
		require.Equal(t, principal, restarted.Principal(), "credentials do not change schedule ownership")
		other, err := authimpl.NewSchedulerSession(t.Context(), &apiReview{}, "other-installation", path, "mcp.example")
		require.NoError(t, err)
		require.NotEqual(t, principal.ID, other.Principal().ID)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, auth.ForwardCredential(request.WithContext(ctx), session), context.Canceled)
	})
}

func TestSchedulerCredentialInvalidProjection(t *testing.T) {
	expires := time.Now().Add(time.Hour).Unix()
	for _, tc := range []struct {
		name, token string
	}{
		{"empty", "\n"},
		{"malformed", "sensitive-invalid-token"},
		{"wrong audience", createTestJWT(map[string]any{"aud": "elsewhere", "exp": expires})},
		{"missing audience", createTestJWT(map[string]any{"exp": expires})},
		{"inbound audience", createTestJWT(map[string]any{"aud": "kagent-invocation", "exp": expires})},
		{"multiple audiences", createTestJWT(map[string]any{"aud": []string{"mcp.example", "kagent-invocation"}, "exp": expires})},
		{"missing expiry", createTestJWT(map[string]any{"aud": "mcp.example"})},
		{"expired", createTestJWT(map[string]any{"aud": "mcp.example", "exp": time.Now().Add(-time.Minute).Unix()})},
		{"not yet valid", createTestJWT(map[string]any{"aud": "mcp.example", "exp": expires, "nbf": expires})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "outbound-token")
			valid := createTestJWT(map[string]any{"aud": "mcp.example", "exp": expires})
			require.NoError(t, os.WriteFile(path, []byte(valid), 0o600))
			session, err := authimpl.NewSchedulerSession(t.Context(), &apiReview{}, "kagent", path, "mcp.example")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte(tc.token), 0o600))
			_, err = authimpl.NewSchedulerSession(t.Context(), &apiReview{}, "kagent", path, "mcp.example")
			require.Error(t, err)
			require.NotContains(t, err.Error(), tc.token)
			request := httptest.NewRequest(http.MethodGet, "http://upstream", nil)
			request.Header.Set("Authorization", "Bearer previous-caller")
			require.Error(t, auth.ForwardCredential(request, session), "rotation must preserve the same checks")
			require.Empty(t, request.Header.Get("Authorization"))
		})
	}
}

func TestSchedulerCredentialConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbound-token")
	token := createTestJWT(map[string]any{"aud": "mcp.example", "exp": time.Now().Add(time.Hour).Unix()})
	require.NoError(t, os.WriteFile(path, []byte(token), 0o600))
	for _, tc := range []struct {
		name, namespace, path, audience string
	}{
		{"missing namespace", "", path, "mcp.example"},
		{"missing path", "kagent", "", "mcp.example"},
		{"missing file", "kagent", path + ".missing", "mcp.example"},
		{"missing audience", "kagent", path, ""},
		{"inbound audience", "kagent", path, "kagent-invocation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authimpl.NewSchedulerSession(t.Context(), &apiReview{}, tc.namespace, tc.path, tc.audience)
			require.Error(t, err)
		})
	}
}

func TestSchedulerCredentialIsolatedFromKubernetesAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbound-token")
	token := createTestJWT(map[string]any{"aud": "mcp.example", "exp": time.Now().Add(time.Hour).Unix()})
	require.NoError(t, os.WriteFile(path, []byte(token), 0o600))
	for _, tc := range []struct {
		name    string
		reviews typedauth.TokenReviewInterface
	}{
		{"accepted by the API", &apiReview{authenticated: true}},
		{"review unavailable", &apiReview{err: errors.New("forbidden")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authimpl.NewSchedulerSession(t.Context(), tc.reviews, "kagent", path, "mcp.example")
			require.Error(t, err)
			require.NotContains(t, err.Error(), token)
		})
	}
	reviews := &apiReview{}
	_, err := authimpl.NewSchedulerSession(t.Context(), reviews, "kagent", path, "mcp.example")
	require.NoError(t, err)
	require.Equal(t, token, reviews.token)
	require.Empty(t, reviews.audiences, "the review must ask about the API's own audiences")
}

// apiReview reports whether the Kubernetes API accepts a token for its own audiences.
type apiReview struct {
	typedauth.TokenReviewInterface
	authenticated bool
	err           error
	token         string
	audiences     []string
}

func (r *apiReview) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	r.token, r.audiences = review.Spec.Token, review.Spec.Audiences
	if r.err != nil {
		return nil, r.err
	}
	return &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{Authenticated: r.authenticated}}, nil
}
