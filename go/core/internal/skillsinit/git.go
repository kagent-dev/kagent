package skillsinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var immutableGitCommit = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// Git calls are bounded twice so a silent peer cannot hang them: git aborts a
// stalled transfer itself, and gitTimeout kills it otherwise. Variables so
// tests can shorten them.
var (
	gitTimeout       = 5 * time.Minute
	gitLowSpeedLimit = "1000" // bytes/sec
	gitLowSpeedTime  = "60"   // seconds
)

// gitSSHCommand makes ssh fail on an unreachable or silent peer.
const gitSSHCommand = "ssh -o ConnectTimeout=30 -o ServerAliveInterval=15 -o ServerAliveCountMax=4"

// CloneGitCommit fetches only one immutable commit instead of cloning the
// repository's complete history. Any existing destination is replaced, so a
// retry after an interrupted fetch starts clean.
func CloneGitCommit(url, commit, destination string) error {
	if !immutableGitCommit.MatchString(commit) {
		return fmt.Errorf("git commit must be a full SHA")
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	if err := runGitIn(destination, "init"); err != nil {
		return err
	}
	if err := runGitIn(destination, "remote", "add", "origin", url); err != nil {
		return err
	}
	if err := runGitIn(destination, "fetch", "--depth", "1", "origin", commit); err != nil {
		return err
	}
	return runGitIn(destination, "checkout", "--detach", "FETCH_HEAD")
}

func runGitIn(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	gitArgs := append([]string{
		"-c", "http.lowSpeedLimit=" + gitLowSpeedLimit,
		"-c", "http.lowSpeedTime=" + gitLowSpeedTime,
	}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = gitEnv(os.Environ())
	// Kill git's helpers (git-remote-https, ssh) along with it.
	configureGitProcess(cmd)
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("git %s timed out after %s: %w", args[0], gitTimeout, err)
	}
	return err
}

// gitEnv adds gitSSHCommand unless the caller already set how git runs ssh.
func gitEnv(env []string) []string {
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_SSH_COMMAND=") || strings.HasPrefix(kv, "GIT_SSH=") {
			return env
		}
	}
	return append(env, "GIT_SSH_COMMAND="+gitSSHCommand)
}
