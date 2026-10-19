package skillsinit

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuthorization is a dummy fixture credential, never a real Secret.
const testAuthorization = "Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0" // x-access-token:secret

// isolateGit keeps developer credential helpers, askpass programs and global
// configuration out of the fetch under test.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
}

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

// requestLog records the Authorization header of every request a server sees.
type requestLog struct {
	mu             sync.Mutex
	authorizations []string
}

func (l *requestLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.authorizations = append(l.authorizations, r.Header.Get("Authorization"))
}

func (l *requestLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.authorizations...)
}

// assertNoCredentialPersisted fails if any file under destination records the
// placeholder, the header configuration or the injected credential.
func assertNoCredentialPersisted(t *testing.T, destination string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(destination, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{GatewayAuthorizationPlaceholder, testAuthorization, "extraHeader", "extraheader"} {
			assert.NotContains(t, string(content), forbidden, path)
		}
		return nil
	}))
}

func TestCloneGitCommitThroughCredentialGateway(t *testing.T) {
	isolateGit(t)
	repository, commit := privateGitServer(t)
	throughGateway := fakeGateway(t, repository)

	t.Run("placeholder is replaced by the gateway", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "skill")
		require.NoError(t, CloneGitCommit(t.Context(), throughGateway, commit, destination, true))
		content, err := os.ReadFile(filepath.Join(destination, "review", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Review", string(content))
		assert.Equal(t, commit, gitOut(t, destination, "rev-parse", "HEAD"))
		assertNoCredentialPersisted(t, destination)
	})

	t.Run("without a placeholder the gateway injects nothing and the fetch fails promptly", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		destination := filepath.Join(t.TempDir(), "skill")
		started := time.Now()
		err := CloneGitCommit(ctx, throughGateway, commit, destination, false)
		require.Error(t, err)
		require.NoError(t, ctx.Err(), "fetch must fail on its own, not wait for a prompt")
		assert.Less(t, time.Since(started), 30*time.Second)
		assert.NoDirExists(t, destination)
	})

	t.Run("the placeholder is not a credential", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "skill")
		err := CloneGitCommit(t.Context(), repository, commit, destination, true)
		require.Error(t, err)
		assert.NoDirExists(t, destination)
	})
}

func TestCloneGitCommitRefusesRedirectsWithAuthorization(t *testing.T) {
	isolateGit(t)
	_, commit := privateGitServer(t)

	var attacker requestLog
	attackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attacker.record(r)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(attackerServer.Close)

	// The authorized upstream redirects to a destination of its choosing.
	var upstream requestLog
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.record(r)
		http.Redirect(w, r, attackerServer.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)

	destination := filepath.Join(t.TempDir(), "skill")
	err := CloneGitCommit(t.Context(), fakeGateway(t, redirecting.URL+"/skills.git"), commit, destination, true)
	require.Error(t, err)
	assert.NoDirExists(t, destination)

	// The configured destination did receive the gateway's credential, so the
	// redirect was the only path to the attacker.
	require.NotEmpty(t, upstream.seen())
	assert.Equal(t, testAuthorization, upstream.seen()[0])
	assert.Empty(t, attacker.seen(), "redirect target must receive no request, let alone an Authorization header")
}

func TestGatewayAuthorizationIsScopedToSourceURL(t *testing.T) {
	isolateGit(t)
	source := "https://forge.example/org/skills.git"
	config := gatewayAuthorizationConfig(source)

	headerFor := func(target string) string {
		out, err := gitCommand(t.Context(), t.TempDir(), config, "config", "--get-urlmatch", "http.extraHeader", target).Output()
		if err != nil {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, 1, exit.ExitCode(), "git config failed: %s", exit.Stderr)
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	assert.Equal(t, "Authorization: "+GatewayAuthorizationPlaceholder, headerFor(source))
	assert.Equal(t, "Authorization: "+GatewayAuthorizationPlaceholder, headerFor(source+"/info/refs"))
	for _, unrelated := range []string{
		"https://attacker.example/org/skills.git",
		"https://forge.example/org/other.git",
		"https://forge.example:8443/org/skills.git",
		"http://forge.example/org/skills.git",
	} {
		assert.Empty(t, headerFor(unrelated), unrelated)
	}
}

func TestCloneGitCommitHonorsCancellation(t *testing.T) {
	isolateGit(t)
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// Cleanups run last-in first-out: release the handler before Close waits
	// for it.
	t.Cleanup(hanging.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	destination := filepath.Join(t.TempDir(), "skill")
	started := time.Now()
	err := CloneGitCommit(ctx, hanging.URL+"/skills.git", strings.Repeat("a", 40), destination, true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 30*time.Second)
	assert.NoDirExists(t, destination)
}
