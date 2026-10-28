package skillsinit

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

var immutableGitCommit = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// CloneGitCommit fetches only one immutable commit instead of cloning the
// repository's complete history.
func CloneGitCommit(url, commit, destination string) error {
	if !immutableGitCommit.MatchString(commit) {
		return fmt.Errorf("git commit must be a full SHA")
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
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}
