package skillsinit

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_applySubPath_rejectsTraversal exercises the validation gate without
// invoking `cp`. We give it a clean dest tree with a real subdir then ask
// for traversal — the function must error before touching the filesystem.
func Test_applySubPath_rejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "real"), 0o755))

	cases := []string{
		"../escape",
		"/etc",
		"a/../../escape",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := applySubPath(dest, p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid subPath")
		})
	}
}

// Test_applySubPath_rejectsNonDir guards against a benign-looking subPath
// that points at a file rather than a directory. Without this check the
// subsequent `cp -rL` would do something silly; the explicit error is
// clearer and matches the documented contract.
func Test_applySubPath_rejectsNonDir(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "file"), []byte("x"), 0o644))

	err := applySubPath(dest, "file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

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

func setGitTimeouts(t *testing.T, overall time.Duration, lowSpeedLimit, lowSpeedTime string) {
	t.Helper()
	oldOverall, oldLimit, oldTime := gitTimeout, gitLowSpeedLimit, gitLowSpeedTime
	gitTimeout, gitLowSpeedLimit, gitLowSpeedTime = overall, lowSpeedLimit, lowSpeedTime
	t.Cleanup(func() { gitTimeout, gitLowSpeedLimit, gitLowSpeedTime = oldOverall, oldLimit, oldTime })
}

func cloneReturns(t *testing.T, wait time.Duration) error {
	t.Helper()
	ref := GitRef{URL: "http://" + stalledServer(t) + "/skills.git", Ref: "main", Dest: filepath.Join(t.TempDir(), "skill")}

	result := make(chan error, 1)
	go func() { result <- CloneGit(ref) }()

	select {
	case err := <-result:
		return err
	case <-time.After(wait):
		t.Fatal("CloneGit still blocked on a silent server")
		return nil
	}
}

func TestCloneGitFailsWhenTransferStalls(t *testing.T) {
	setGitTimeouts(t, time.Minute, "1000", "1")

	err := cloneReturns(t, 15*time.Second)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "timed out", "git should give up before the overall deadline")
}

func TestCloneGitKillsGitAtOverallDeadline(t *testing.T) {
	// A limit of 0 disables git's own stall detection.
	setGitTimeouts(t, time.Second, "0", "60")

	err := cloneReturns(t, 15*time.Second)
	require.ErrorContains(t, err, "timed out")
}

func newSourceRepo(t *testing.T) string {
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
	git("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0o600))
	git("add", ".")
	git("commit", "-q", "-m", "skill")
	return "file://" + dir
}

func TestCloneGitClonesBranch(t *testing.T) {
	url := newSourceRepo(t)
	dest := filepath.Join(t.TempDir(), "skill")

	require.NoError(t, CloneGit(GitRef{URL: url, Ref: "main", Dest: dest}))

	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "skill", string(content))
}

func TestCloneGitReplacesInterruptedDestination(t *testing.T) {
	url := newSourceRepo(t)
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "partial"), []byte("x"), 0o600))

	require.NoError(t, CloneGit(GitRef{URL: url, Ref: "main", Dest: dest}))
	require.NoFileExists(t, filepath.Join(dest, "partial"))
	require.FileExists(t, filepath.Join(dest, "SKILL.md"))
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
