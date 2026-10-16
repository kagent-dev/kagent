//go:build linux

package bridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/supervisor"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestSupervisorDeathReapsDetachedTools(t *testing.T) {
	for _, stage := range []string{"payload", "guard"} {
		t.Run(stage, func(t *testing.T) { testSupervisorDeath(t, stage) })
	}
}

func testSupervisorDeath(t *testing.T, stage string) {
	t.Helper()
	sentinel := exec.Command("/bin/sleep", "60")
	require.NoError(t, sentinel.Start())
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() })
	executable, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	script, pidFile := filepath.Join(dir, "dsh"), filepath.Join(dir, "tool.pid")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\\''")+"' -test.run=TestDetachedDSHProcess\n"), 0700))
	cfg, err := config.Parse([]byte(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"}}`))
	require.NoError(t, err)
	runner, err := New(cfg, script, filepath.Join(dir, "data"), append(os.Environ(), "DSH_AUDIT_DETACHED="+pidFile, "DSH_AUDIT_EXIT=forced"))
	require.NoError(t, err)
	var dshPID, toolPID int
	t.Cleanup(func() {
		if dshPID > 0 {
			_ = syscall.Kill(-dshPID, syscall.SIGKILL)
		}
		if toolPID > 0 {
			_ = syscall.Kill(-toolPID, syscall.SIGKILL)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, runtime.Turn{Prompt: "work"}, &recordingSink{}); done <- err }()
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		toolPID, err = strconv.Atoi(string(data))
		if err != nil {
			return false
		}
		data, err = os.ReadFile(pidFile + ".dsh")
		if err != nil {
			return false
		}
		dshPID, err = strconv.Atoi(string(data))
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	supervisorPID := parentPID(t, dshPID)
	if stage == "guard" {
		supervisorPID = parentPID(t, supervisorPID)
	}
	require.Greater(t, supervisorPID, 1)
	commandLine, err := os.ReadFile("/proc/" + strconv.Itoa(supervisorPID) + "/cmdline")
	require.NoError(t, err)
	require.Contains(t, string(commandLine), supervisor.Mode+"\x00"+stage+"\x00", "only kill the expected supervisor")
	require.NoError(t, syscall.Kill(supervisorPID, syscall.SIGKILL))
	cancel()
	returned := false
	select {
	case err := <-done:
		returned = true
		require.Error(t, err)
	case <-time.After(12 * time.Second):
	}
	dshAlive, toolAlive := syscall.Kill(dshPID, 0) == nil, syscall.Kill(toolPID, 0) == nil
	require.NoError(t, syscall.Kill(sentinel.Process.Pid, 0), "unrelated process must survive")
	require.True(t, returned, "supervisor failure leaves Run blocked despite cancellation")
	require.False(t, dshAlive || toolAlive, "failed supervisor must not release a turn while its payload is alive")
}

func parentPID(t *testing.T, pid int) int {
	t.Helper()
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(status), "\n") {
		if value, ok := strings.CutPrefix(line, "PPid:"); ok {
			parent, err := strconv.Atoi(strings.TrimSpace(value))
			require.NoError(t, err)
			return parent
		}
	}
	t.Fatal("missing PPid")
	return 0
}

func TestMissingCleanupConfirmationBlocksCompletion(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	logPath := filepath.Join(t.TempDir(), "supervisor.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	defer logFile.Close()
	owner := exec.Command(executable, "-test.run=^TestMissingCleanupOwner$")
	owner.Env = append(os.Environ(), "DSH_TEST_MISSING_CONFIRMATION=1")
	owner.Stderr = logFile
	require.NoError(t, owner.Start())
	done := make(chan error, 1)
	go func() { done <- owner.Wait() }()
	t.Cleanup(func() { _ = owner.Process.Kill(); <-done })
	require.Eventually(t, func() bool {
		output, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(output), "actor replacement required")
	}, 5*time.Second, 10*time.Millisecond)
	select {
	case err := <-done:
		done <- err
		t.Fatal("missing cleanup confirmation must not settle the turn", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMissingCleanupOwner(t *testing.T) {
	if os.Getenv("DSH_TEST_MISSING_CONFIRMATION") == "" {
		return
	}
	// A process that exits without implementing the private confirmation protocol.
	control, err := supervisor.Start(exec.Command("/bin/true"))
	require.NoError(t, err)
	defer control.Close()
	_ = control.Wait()
	os.Exit(3)
}
