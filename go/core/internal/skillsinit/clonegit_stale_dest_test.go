package skillsinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_CloneGit_cleansStaleDest guards against a regression of the bug where
// a prior failed attempt (e.g. killed between the clone/checkout and
// applySubPath's final rename) leaves ref.Dest non-empty on disk. Without
// cleanup, every subsequent retry fails git's own pre-flight
// "already exists and is not an empty directory" check forever, even though
// the container is documented to retry from scratch on failure.
func Test_CloneGit_cleansStaleDest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	origin := t.TempDir()
	runIn(t, origin, "init", "--initial-branch=main")
	runIn(t, origin, "config", "user.email", "test@example.com")
	runIn(t, origin, "config", "user.name", "test")
	require.NoError(t, os.WriteFile(filepath.Join(origin, "README.md"), []byte("hi"), 0o644))
	runIn(t, origin, "add", "README.md")
	runIn(t, origin, "commit", "-m", "init")

	dest := filepath.Join(t.TempDir(), "dest")
	// Simulate the leftover from an interrupted prior attempt: a non-empty
	// dest that was never cleaned up.
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("leftover"), 0o644))

	err := CloneGit(GitRef{URL: origin, Ref: "main", Dest: dest})
	require.NoError(t, err, "CloneGit must clean a stale dest and retry successfully")

	_, err = os.Stat(filepath.Join(dest, "README.md"))
	require.NoError(t, err, "dest should contain the freshly cloned content")
	_, err = os.Stat(filepath.Join(dest, "stale.txt"))
	require.True(t, os.IsNotExist(err), "stale leftover content must not survive the clone")
}

func runIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
