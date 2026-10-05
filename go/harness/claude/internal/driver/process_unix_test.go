//go:build unix

package driver

import (
	"os/exec"
	"testing"
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
