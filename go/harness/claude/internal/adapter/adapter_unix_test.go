//go:build unix

package adapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
)

// Run as root with only CHOWN, SETGID and SETUID to prove the capability set
// the claude translator asks for.
func TestNewHandsClaudesTreesToTheUnprivilegedUserOnEveryStart(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the harness hands its trees over only when it runs as root")
	}
	base := t.TempDir()
	t.Cleanup(func() { _ = utils.ReclaimTree(base) })
	for _, dir := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(dir, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	durableDir := filepath.Join(base, "data")
	input := Input{
		ConfigJSON: []byte(`{"version":5,"claude_executable":"claude","expected_claude_version":"2.1.260","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`),
		Workspace:  filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: filepath.Join(base, "ephemeral"), Environment: []string{"PATH=/bin:/usr/bin"},
	}
	start := func() {
		t.Helper()
		if _, err := New(t.Context(), input); err != nil {
			t.Fatalf("New() = %v", err)
		}
		for _, tree := range []string{input.Workspace, filepath.Join(durableDir, "claude"), input.EphemeralDir} {
			if uid := owner(t, tree); uid != config.UnprivilegedUID {
				t.Errorf("%s belongs to %d, want the unprivileged user", tree, uid)
			}
		}
		info, err := os.Lstat(durableDir)
		if err != nil {
			t.Fatal(err)
		}
		if owner(t, durableDir) != 0 || info.Mode().Perm()&0o011 != 0o011 {
			t.Errorf("the durable directory is the harness's and searchable, got uid %d mode %o", owner(t, durableDir), info.Mode().Perm())
		}
	}
	start()
	harnessState := filepath.Join(durableDir, "adapter")
	if err := utils.EnsurePrivateDir(harnessState); err != nil {
		t.Fatal(err)
	}

	output := asClaude(t, input.Workspace, `set -e
id -G
mkdir -p repo/.git && echo ref > repo/.git/HEAD && chmod 600 repo/.git/HEAD && chmod 000 repo
if mv "$1" "$1.moved" 2>/dev/null; then echo "renamed the harness state"; exit 1; fi`, harnessState)
	if groups := strings.Fields(output); len(groups) != 1 || groups[0] != "65532" {
		t.Fatalf("Claude runs with groups %v, want only its own", groups)
	}

	start()
	asClaude(t, input.Workspace, `set -e
test -O repo/.git/HEAD || { echo "a file Claude wrote is no longer Claude's"; exit 1; }
test -r repo -a -w repo -a -x repo || { echo "a directory Claude locked is not reopened"; exit 1; }`)
}

func asClaude(t *testing.T, dir, script string, args ...string) string {
	t.Helper()
	claude := exec.Command("/bin/sh", append([]string{"-c", script, "sh"}, args...)...)
	claude.Dir = dir
	utils.RunAs(claude, config.UnprivilegedUID, config.UnprivilegedGID)
	output, err := claude.CombinedOutput()
	if err != nil {
		t.Fatalf("Claude's turn: %v: %s", err, output)
	}
	return string(output)
}

func owner(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Uid)
}
