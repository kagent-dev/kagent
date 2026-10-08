package skillsinit

import (
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAuthorization = "Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0" // x-access-token:secret

// privateGitServer serves one repository over Git smart HTTP and rejects every
// request without the expected Authorization header, like a private forge.
func privateGitServer(t *testing.T) (serverURL, commit string) {
	t.Helper()
	root := t.TempDir()
	repository := filepath.Join(root, "skills.git")
	require.NoError(t, os.MkdirAll(filepath.Join(repository, "review"), 0o755))
	gitIn(t, repository, "init", "-q")
	gitIn(t, repository, "config", "user.email", "t@example.com")
	gitIn(t, repository, "config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(repository, "review", "SKILL.md"), []byte("# Review"), 0o644))
	gitIn(t, repository, "add", ".")
	gitIn(t, repository, "commit", "-qm", "skill")
	commit = gitOut(t, repository, "rev-parse", "HEAD")
	gitIn(t, repository, "commit", "-qm", "later", "--allow-empty")

	execPath, err := exec.Command("git", "--exec-path").Output()
	require.NoError(t, err)
	backend := &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != testAuthorization {
			w.Header().Set("WWW-Authenticate", `Basic realm="private"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/skills.git", commit
}

// fakeGateway forwards to target and, like Substrate's egress gateway,
// overwrites the Authorization header only on requests that already carry one.
func fakeGateway(t *testing.T, target string) string {
	t.Helper()
	upstream, err := url.Parse(target)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: upstream.Scheme, Host: upstream.Host})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			r.Header.Set("Authorization", testAuthorization)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	gatewayURL, err := url.Parse(gateway.URL)
	require.NoError(t, err)
	upstream.Host = gatewayURL.Host
	return upstream.String()
}

func TestCloneGitCommitThroughCredentialGateway(t *testing.T) {
	// Isolate from any developer credential helper or global configuration.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repository, commit := privateGitServer(t)
	throughGateway := fakeGateway(t, repository)

	t.Run("placeholder is replaced by the gateway", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "skill")
		require.NoError(t, CloneGitCommit(throughGateway, commit, destination, true))
		content, err := os.ReadFile(filepath.Join(destination, "review", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Review", string(content))
		assert.Equal(t, commit, gitOut(t, destination, "rev-parse", "HEAD"))
		config, err := os.ReadFile(filepath.Join(destination, ".git", "config"))
		require.NoError(t, err)
		assert.NotContains(t, string(config), GatewayAuthorizationPlaceholder)
		assert.NotContains(t, string(config), "extraHeader")
	})

	t.Run("without a placeholder the gateway injects nothing", func(t *testing.T) {
		err := CloneGitCommit(throughGateway, commit, filepath.Join(t.TempDir(), "skill"), false)
		require.Error(t, err)
	})

	t.Run("the placeholder is not a credential", func(t *testing.T) {
		err := CloneGitCommit(repository, commit, filepath.Join(t.TempDir(), "skill"), true)
		require.Error(t, err)
	})
}
