package skillsinit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit("https://example.com/repository.git", "main", t.TempDir(), false)
	require.ErrorContains(t, err, "full SHA")
}

// TestCloneGitCommitAuthorizationPlaceholder guards the egress contract: the
// gateway replaces an Authorization header the request carries and adds none,
// so an authenticated source must send the placeholder on its first request,
// and a public source must send nothing. The server answers 401 like a private
// repository without a credential; git must fail without prompting.
func TestCloneGitCommitAuthorizationPlaceholder(t *testing.T) {
	for _, authenticated := range []bool{true, false} {
		t.Run(fmt.Sprintf("authenticated=%t", authenticated), func(t *testing.T) {
			var mu sync.Mutex
			var seen []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Header.Get("Authorization"))
				mu.Unlock()
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()

			err := CloneGitCommit(server.URL+"/org/private", strings.Repeat("a", 40), filepath.Join(t.TempDir(), "dest"), authenticated)
			require.Error(t, err)

			mu.Lock()
			defer mu.Unlock()
			require.NotEmpty(t, seen)
			want := ""
			if authenticated {
				want = credentialPlaceholder
			}
			assert.Equal(t, want, seen[0])
		})
	}
}

func TestGitEnvironmentScopesThePlaceholderToTheSource(t *testing.T) {
	assert.Equal(t, []string{"GIT_TERMINAL_PROMPT=0"}, gitEnvironment("https://github.com/org/public", false))
	assert.Equal(t, []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/org/private.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic kagent-credential-injected",
	}, gitEnvironment("https://github.com/org/private", true))
}
