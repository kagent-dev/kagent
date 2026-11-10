package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	authpkg "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCapturedCredentialExpiresBeforeForwarding(t *testing.T) {
	session := authpkg.NewCredentialSession(authpkg.Principal{ID: "caller"}, bearerCredential{
		authHeader: "Bearer previously-verified", expiresAt: time.Now().Add(-time.Second),
	})
	request := httptest.NewRequest(http.MethodPost, "http://runtime", nil)
	request.Header.Set("Authorization", "Bearer stale")
	require.ErrorIs(t, authpkg.ForwardCredential(request, session), ErrUnauthenticated)
	require.Empty(t, request.Header.Get("Authorization"))
}
