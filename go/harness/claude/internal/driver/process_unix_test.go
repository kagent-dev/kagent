//go:build unix

package driver

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/workspace"
)

func TestProcessDriverRunsClaudeAsTheConfiguredUser(t *testing.T) {
	d := NewProcessDriver(ProcessConfig{Executable: "claude", Workspace: t.TempDir(), RunAs: &Identity{UID: 65532, GID: 65532}})
	cmd := exec.Command("claude")
	d.runAs(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil || cmd.SysProcAttr.Credential.Uid != 65532 || cmd.SysProcAttr.Credential.Gid != 65532 || cmd.SysProcAttr.Credential.NoSetGroups || len(cmd.SysProcAttr.Credential.Groups) != 0 {
		t.Fatalf("Claude does not run as the configured user: %#v", cmd.SysProcAttr)
	}
	plain := exec.Command("claude")
	NewProcessDriver(ProcessConfig{Executable: "claude"}).runAs(plain)
	if plain.SysProcAttr != nil {
		t.Fatalf("a driver without RunAs changes the process credential: %#v", plain.SysProcAttr)
	}
}

// A session directory without mirrors is handed to Claude's user too: the
// volume created it for the container's user, root. The test hands it to a
// supplementary group of its own user, which any user may do.
func TestProcessDriverHandsTheSessionDirectoryOverWithoutMirrors(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	gid := -1
	for _, group := range groups {
		if group != os.Getgid() {
			gid = group
			break
		}
	}
	if gid < 0 {
		t.Skip("the test user has no supplementary group to hand the directory to")
	}
	dir := t.TempDir()
	d := NewProcessDriver(ProcessConfig{Executable: "claude", Workspace: t.TempDir(), RunAs: &Identity{UID: uint32(os.Getuid()), GID: uint32(gid)}})
	report, err := d.prepareAsUser(t.Context(), workspace.Dispatch{Directory: dir}, workspace.Options{})
	if err != nil || report.SetUp {
		t.Fatalf("prepareAsUser() = %#v, %v; want no setup and no error", report, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := int(info.Sys().(*syscall.Stat_t).Gid); got != gid {
		t.Errorf("the session directory's group = %d, want %d", got, gid)
	}
}
