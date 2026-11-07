package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func CommitFiles(t testing.TB, files map[string]string) (repository, commit string) {
	t.Helper()
	repository = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", repository}, args...)...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	git("config", "commit.gpgsign", "false")
	for name, content := range files {
		path := filepath.Join(repository, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	return repository, git("rev-parse", "HEAD")
}
