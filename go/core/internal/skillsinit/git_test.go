package skillsinit

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit("https://example.com/repository.git", "main", t.TempDir())
	require.ErrorContains(t, err, "full SHA")
}

// stalledServer accepts connections and never replies, like a dropped flow.
func stalledServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// setGitTimeouts shortens the stall settings for one test.
func setGitTimeouts(t *testing.T, overall time.Duration, lowSpeedLimit, lowSpeedTime string) {
	t.Helper()
	oldOverall, oldLimit, oldTime := gitTimeout, gitLowSpeedLimit, gitLowSpeedTime
	gitTimeout, gitLowSpeedLimit, gitLowSpeedTime = overall, lowSpeedLimit, lowSpeedTime
	t.Cleanup(func() { gitTimeout, gitLowSpeedLimit, gitLowSpeedTime = oldOverall, oldLimit, oldTime })
}

// cloneReturns runs CloneGitCommit against a stalled server.
func cloneReturns(t *testing.T, wait time.Duration) error {
	t.Helper()
	url := "http://" + stalledServer(t) + "/skills.git"

	result := make(chan error, 1)
	go func() { result <- CloneGitCommit(url, strings.Repeat("a", 40), t.TempDir()) }()

	select {
	case err := <-result:
		return err
	case <-time.After(wait):
		t.Fatal("CloneGitCommit still blocked on a silent server")
		return nil
	}
}

func TestCloneGitCommitFailsWhenTransferStalls(t *testing.T) {
	setGitTimeouts(t, time.Minute, "1000", "1")

	err := cloneReturns(t, 15*time.Second)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "timed out", "git should give up before the overall deadline")
}

func TestCloneGitCommitKillsGitAtOverallDeadline(t *testing.T) {
	// A limit of 0 disables git's own stall detection.
	setGitTimeouts(t, time.Second, "0", "60")

	err := cloneReturns(t, 15*time.Second)
	require.ErrorContains(t, err, "timed out")
}

// newSourceRepo creates a repository with one commit and returns its file URL
// and SHA.
func newSourceRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0o600))
	git("add", ".")
	git("commit", "-q", "-m", "skill")
	return "file://" + dir, git("rev-parse", "HEAD")
}

func TestCloneGitCommitChecksOutCommit(t *testing.T) {
	url, commit := newSourceRepo(t)
	destination := filepath.Join(t.TempDir(), "skill")

	require.NoError(t, CloneGitCommit(url, commit, destination))

	content, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "skill", string(content))
}

func TestCloneGitCommitReplacesInterruptedDestination(t *testing.T) {
	url, commit := newSourceRepo(t)
	destination := t.TempDir()

	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", url}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = destination
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(destination, "partial"), []byte("x"), 0o600))

	require.NoError(t, CloneGitCommit(url, commit, destination))
	require.NoFileExists(t, filepath.Join(destination, "partial"))
	require.FileExists(t, filepath.Join(destination, "SKILL.md"))
}

func TestGitEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{"adds keepalives", []string{"A=1"}, []string{"A=1", "GIT_SSH_COMMAND=" + gitSSHCommand}},
		{"keeps GIT_SSH_COMMAND", []string{"GIT_SSH_COMMAND=ssh -i key"}, []string{"GIT_SSH_COMMAND=ssh -i key"}},
		{"keeps GIT_SSH", []string{"GIT_SSH=/bin/wrapper"}, []string{"GIT_SSH=/bin/wrapper"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gitEnv(tt.env))
		})
	}
}
