package skillsinit

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	if err := runGitWith(destination, env, "fetch", "--depth", "1", "origin", commit); err != nil {
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
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}
