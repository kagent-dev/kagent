//go:build unix

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecuteCommand_TimeoutKillsChildProcesses(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	// sleep runs as a child of bash, so killing bash alone leaves sleep
	// running and holding the output pipe until it finishes.
	start := time.Now()
	_, err := NewCommandExecutor().executeCommand(context.Background(), "sleep 20 & echo $! > pid; wait; echo done", tmpDir, 500*time.Millisecond)
	elapsed := time.Since(start)
	sleepPid := readPid(t, filepath.Join(tmpDir, "pid"))

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Expected a timeout error, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Timeout of 500ms returned after %v", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(sleepPid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("Child process %d is still running after the timeout", sleepPid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExecuteCommand_BackgroundProcessDoesNotBlock(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	// The shell exits at once, but the background job inherits stdout and
	// keeps it open for as long as it runs.
	start := time.Now()
	result, err := NewCommandExecutor().executeCommand(context.Background(), "sleep 20 & echo $! > pid; echo started", tmpDir, 30*time.Second)
	elapsed := time.Since(start)
	readPid(t, filepath.Join(tmpDir, "pid"))

	if err != nil {
		t.Fatalf("Expected the command to succeed, got %v", err)
	}
	if result != "started" {
		t.Errorf("Expected output %q, got %q", "started", result)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Command returned after %v, waiting on the background job", elapsed)
	}
}

// readPid reads a pid a test command wrote, and kills that process when the
// test ends so nothing it started outlives the test.
func readPid(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read pid file: %v", err)
	}
	var pid int
	if _, err := fmt.Sscan(string(content), &pid); err != nil {
		t.Fatalf("Failed to parse pid %q: %v", content, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}
