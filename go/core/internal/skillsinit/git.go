package skillsinit

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"
)

var immutableGitCommit = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// credentialPlaceholder is the Authorization value git sends for an
// authenticated source. The egress gateway replaces the whole header with the
// source's credential and never inspects the placeholder.
const credentialPlaceholder = "Basic kagent-credential-injected"

// CloneGitCommit fetches only one immutable commit instead of cloning the
// repository's complete history. An authenticated source sends a placeholder
// Authorization header for its URL only: the egress gateway replaces a
// credential header a request carries and leaves a request without one
// untouched, and git sends none of its own before a challenge.
func CloneGitCommit(url, commit, destination string, authenticated bool) error {
	if !immutableGitCommit.MatchString(commit) {
		return fmt.Errorf("git commit must be a full SHA")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	env := gitEnvironment(url, authenticated)
	if err := runGitWith(destination, env, "init"); err != nil {
		return err
	}
	if err := runGitWith(destination, env, "remote", "add", "origin", url); err != nil {
		return err
	}
	if err := runGitRetrying(destination, env, "fetch", "--depth", "1", "origin", commit); err != nil {
		return err
	}
	return runGitWith(destination, env, "checkout", "--detach", "FETCH_HEAD")
}

// gitEnvironment returns the environment additions for fetching url. Git never
// prompts: a refused credential fails with its error instead of waiting on a
// terminal the actor does not have. An authenticated source gets the
// placeholder header through git's per-invocation configuration, scoped to its
// URL and never written to a gitconfig.
func gitEnvironment(url string, authenticated bool) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if authenticated {
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http."+url+".extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: "+credentialPlaceholder,
		)
	}
	return env
}

func runGitWith(dir string, env []string, args ...string) error {
	return gitCommand(dir, env, os.Stderr, args...).Run()
}

func gitCommand(dir string, env []string, stderr io.Writer, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

// fetchAttempts bounds how often a network step of git runs when it fails on
// a transient network error; fetchBackoff is the wait before the second
// attempt, doubled before each later one (1 s, 2 s, 4 s). No attempt starts
// once fetchBudget has passed since the first, so the retries end inside the
// 30 s a runtime has to serve its readiness probe.
var (
	fetchAttempts = 4
	fetchBackoff  = time.Second
	fetchBudget   = 15 * time.Second
)

// transientGitFailure matches git's error for a failure a later attempt can
// outlive: the name does not resolve yet, the connection is refused, times out
// or drops, an HTTP/2 stream is refused or reset, the transfer is cut, or a
// gateway or server answers 5xx. A refused credential (401, 403), a missing
// repository (404) or an unknown commit matches none of them and fails at
// once.
var transientGitFailure = regexp.MustCompile(`Could not resolve host|Failed to connect to|Connection refused|Connection timed out|Operation timed out|Connection reset by peer|Empty reply from server|The requested URL returned error: 5[0-9]{2}|RPC failed; HTTP 5[0-9]{2}|HTTP/2 stream [0-9]+ was (not closed cleanly|reset)|HTTP/2 framing layer|Recv failure|SSL_read|SSL_ERROR_SYSCALL|GnuTLS recv error|transfer closed with outstanding read data remaining|early EOF|the remote end hung up unexpectedly`)

// runGitRetrying runs a network step of git and runs it again, up to
// fetchAttempts times and within fetchBudget, while it fails on a transient
// network error.
func runGitRetrying(dir string, env []string, args ...string) error {
	wait := fetchBackoff
	deadline := time.Now().Add(fetchBudget)
	for attempt := 1; ; attempt++ {
		var stderr bytes.Buffer
		err := gitCommand(dir, env, io.MultiWriter(os.Stderr, &stderr), args...).Run()
		if err == nil || attempt == fetchAttempts || !transientGitFailure.Match(stderr.Bytes()) {
			return err
		}
		if time.Now().Add(wait).After(deadline) {
			fmt.Fprintf(os.Stderr, "git %s failed on a transient network error (attempt %d of %d); the %s retry budget is spent\n", args[0], attempt, fetchAttempts, fetchBudget)
			return err
		}
		fmt.Fprintf(os.Stderr, "git %s failed on a transient network error (attempt %d of %d), retrying in %s\n", args[0], attempt, fetchAttempts, wait)
		time.Sleep(wait)
		wait *= 2
	}
}
