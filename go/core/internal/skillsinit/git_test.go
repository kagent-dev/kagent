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

// TestCloneGit_fullChecksOutTheCommit guards the `Full` path, which is what a
// commit-SHA skill ref takes. The checkout must resolve the ref as a revision:
// with a `--` separator git reads it as a pathspec instead, so the SHA is
// looked up as a file name and the clone fails with "pathspec ... did not match
// any file(s) known to git".
func TestCloneGit_fullChecksOutTheCommit(t *testing.T) {
	repository := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repository}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("# first"), 0o644))
	git("add", ".")
	git("commit", "-q", "-m", "first")
	first := git("rev-parse", "HEAD")
	// A second commit, so the clone's tip is not the commit under test: a
	// checkout that silently did nothing would leave the wrong tree behind.
	require.NoError(t, os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("# second"), 0o644))
	git("commit", "-q", "-am", "second")

	dest := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, CloneGit(GitRef{URL: repository, Ref: first, Dest: dest, Full: true}))

	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "# first", string(content), "the clone must be at the requested commit")
}
