package skillsinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit("https://example.com/repository.git", "main", t.TempDir())
	require.ErrorContains(t, err, "full SHA")
}

// TestCloneGit_fullCheckoutBySHA guards #2608: a Full clone with a commit
// SHA ref must check the SHA out as a revision. Passing it after `--`
// made git treat it as a pathspec and fail with "did not match any
// file(s) known to git".
func TestCloneGit_fullCheckoutBySHA(t *testing.T) {
	src := t.TempDir()
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "config", "user.email", "t@example.com")
	gitIn(t, src, "config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644))
	gitIn(t, src, "add", "f")
	gitIn(t, src, "commit", "-qm", "init")
	sha := gitOut(t, src, "rev-parse", "HEAD")
	require.Len(t, sha, 40)

	dest := filepath.Join(t.TempDir(), "dest")
	require.NoError(t, CloneGit(GitRef{URL: src, Ref: sha, Dest: dest, Full: true}))
	got, err := os.ReadFile(filepath.Join(dest, "f"))
	require.NoError(t, err)
	assert.Equal(t, []byte("x"), got)
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
