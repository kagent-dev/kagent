package skillsinit

import (
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// TestCloneGitCommitRetriesATransientFailure guards the golden boot against a
// dropped first connection: the server answers the first request with a 503,
// as the egress gateway does when its connection to the origin times out, and
// serves the repository afterwards. The fetch must succeed within one call.
func TestCloneGitCommitRetriesATransientFailure(t *testing.T) {
	setFetchBackoff(t, 10*time.Millisecond)
	commit := servedRepository(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		commit.backend.ServeHTTP(w, r)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "dest")
	require.NoError(t, CloneGitCommit(server.URL+"/skill.git", commit.sha, dest, false))
	got, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "skill\n", string(got))
	assert.Equal(t, commit.sha, gitOut(t, dest, "rev-parse", "HEAD"))
}

// TestCloneGitCommitFailsAtOnceOnAPermanentFailure guards the other side: a
// refused credential or a missing repository is no transient failure, so the
// fetch fails with git's error and never waits for a second attempt.
func TestCloneGitCommitFailsAtOnceOnAPermanentFailure(t *testing.T) {
	setFetchBackoff(t, time.Minute)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			start := time.Now()
			err := CloneGitCommit(server.URL+"/skill.git", strings.Repeat("a", 40), filepath.Join(t.TempDir(), "dest"), false)
			require.Error(t, err)
			assert.Less(t, time.Since(start), fetchBackoff)
		})
	}
}

// TestTransientGitFailure pins the classification on git's own error lines.
func TestTransientGitFailure(t *testing.T) {
	for line, transient := range map[string]bool{
		"fatal: unable to access 'https://origin/skill.git/': The requested URL returned error: 503":                          true,
		"fatal: unable to access 'https://origin/skill.git/': Could not resolve host: origin":                                 true,
		"fatal: unable to access 'https://origin/skill.git/': Failed to connect to origin port 443: Connection refused":       true,
		"fatal: unable to access 'https://origin/skill.git/': Connection timed out after 10001 milliseconds":                  true,
		"error: RPC failed; HTTP 502 curl 22 The requested URL returned error: 502":                                           true,
		"fatal: unable to access 'https://origin/skill.git/': HTTP/2 stream 1 was not closed cleanly: REFUSED_STREAM (err 7)": true,
		"fatal: unable to access 'https://origin/skill.git/': HTTP/2 stream 3 was reset":                                      true,
		"error: RPC failed; curl 18 transfer closed with outstanding read data remaining":                                     true,
		"fatal: unable to access 'https://origin/skill.git/': Recv failure: Connection reset by peer":                         true,
		"fatal: early EOF":                           true,
		"fatal: the remote end hung up unexpectedly": true,
		"fatal: unable to access 'https://origin/skill.git/': The requested URL returned error: 401": false,
		"fatal: unable to access 'https://origin/skill.git/': The requested URL returned error: 403": false,
		"remote: Repository not found.\nfatal: repository 'https://origin/skill.git/' not found":     false,
		"fatal: remote error: upload-pack: not our ref aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":     false,
		"fatal: could not read Username for 'https://origin': terminal prompts disabled":             false,
	} {
		assert.Equal(t, transient, transientGitFailure.MatchString(line), line)
	}
}

// TestCloneGitCommitStopsRetryingOnceTheBudgetIsSpent keeps the retries
// inside a runtime's readiness probe: a source that keeps failing is given up
// once the next attempt would start after the budget, even with attempts left.
func TestCloneGitCommitStopsRetryingOnceTheBudgetIsSpent(t *testing.T) {
	setFetchBackoff(t, 400*time.Millisecond)
	previous := fetchBudget
	fetchBudget = time.Second
	t.Cleanup(func() { fetchBudget = previous })

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info/refs") {
			attempts.Add(1)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := CloneGitCommit(server.URL+"/skill.git", strings.Repeat("a", 40), filepath.Join(t.TempDir(), "dest"), false)
	require.Error(t, err)
	// Attempt 1, wait 400 ms, attempt 2; the 800 ms wait before attempt 3
	// ends past the 1 s budget.
	assert.Equal(t, int32(2), attempts.Load())
}

func setFetchBackoff(t *testing.T, backoff time.Duration) {
	t.Helper()
	previous := fetchBackoff
	fetchBackoff = backoff
	t.Cleanup(func() { fetchBackoff = previous })
}

type servedCommit struct {
	sha     string
	backend http.Handler
}

// servedRepository creates a repository with one commit and git's smart HTTP
// backend serving it at /skill.git.
func servedRepository(t *testing.T) servedCommit {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	root := t.TempDir()
	src := filepath.Join(root, "skill.git")
	require.NoError(t, os.Mkdir(src, 0o755))
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "config", "user.email", "t@example.com")
	gitIn(t, src, "config", "user.name", "t")
	gitIn(t, src, "config", "commit.gpgsign", "false")
	gitIn(t, src, "config", "uploadpack.allowAnySHA1InWant", "true")
	require.NoError(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("skill\n"), 0o644))
	gitIn(t, src, "add", "SKILL.md")
	gitIn(t, src, "commit", "-qm", "skill")
	return servedCommit{
		sha: gitOut(t, src, "rev-parse", "HEAD"),
		backend: &cgi.Handler{
			Path: gitPath,
			Args: []string{"http-backend"},
			Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
		},
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
