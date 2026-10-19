package skillsinit

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)
	return strings.TrimSpace(string(out))
}

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit(t.Context(), "https://example.com/repository.git", "main", t.TempDir(), false)
	require.ErrorContains(t, err, "full SHA")
}
