//go:build unix

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
)

func TestExecuteCommand_TimeoutKillsChildProcesses(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	// sleep runs as a child of bash, so killing bash alone leaves sleep
	// running and holding the output pipe until it finishes.
	start := time.Now()
	_, err := NewCommandExecutor(time.Minute).executeCommand(context.Background(), "sleep 20 & echo $! > pid; wait; echo done", tmpDir, 500*time.Millisecond)
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
	result, err := NewCommandExecutor(time.Minute).executeCommand(context.Background(), "sleep 20 & echo $! > pid; echo started", tmpDir, 30*time.Second)
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

func TestExecuteCommand_BackgroundProcessKeepsWritingAfterReturn(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	// A server started in the background logs to the inherited stdout and
	// stderr long after the call returns. Those writes must keep succeeding:
	// if the pipes were closed when the call returned, the next write would
	// fail with EPIPE (bash is killed by SIGPIPE here and never writes the
	// marker; a Python http.server drops the request it is logging).
	script := "(sleep 3; echo out; echo err >&2; echo ok > marker) & echo $! > pid; echo started"
	result, err := NewCommandExecutor(time.Minute).executeCommand(context.Background(), script, tmpDir, 30*time.Second)
	readPid(t, filepath.Join(tmpDir, "pid"))
	if err != nil {
		t.Fatalf("Expected the command to succeed, got %v", err)
	}
	if result != "started" {
		t.Errorf("Expected output %q, got %q", "started", result)
	}

	marker := filepath.Join(tmpDir, "marker")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Background job did not finish; it could not write to its inherited output")
		}
		time.Sleep(50 * time.Millisecond)
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

// TestExecuteCommand_TimeoutBoundsTheCommand runs the commands kagent-dev/kagent#3006
// measured under the timeout KAGENT_BASH_TOOL_TIMEOUT=2s configures. Each
// command first records the shell's PID, the ID of its process group.
func TestExecuteCommand_TimeoutBoundsTheCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantResult  string
		wantTimeout bool
		maxElapsed  time.Duration
		// groupGone is whether every process the command started has exited
		// when the call returns. A background job outlives the call; the turn
		// ends it.
		groupGone bool
	}{
		{
			name:        "foreground command past the timeout",
			command:     "echo $$ > pgid; sleep 8; echo done",
			wantTimeout: true,
			maxElapsed:  4 * time.Second,
			groupGone:   true,
		},
		{
			// The call returns without waiting for the job, once output
			// collection gives up on the pipe the job holds open.
			name:       "background job does not hold the call",
			command:    "echo $$ > pgid; sleep 6 & echo started",
			wantResult: "started",
			maxElapsed: commandWaitDelay + time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			start := time.Now()
			result, err := NewCommandExecutor(2*time.Second).ExecuteCommand(context.Background(), tt.command, dir)
			elapsed := time.Since(start)
			pgid := readProcessGroup(t, filepath.Join(dir, "pgid"))

			if tt.wantTimeout {
				if err == nil || !strings.Contains(err.Error(), "timed out after 2s") {
					t.Fatalf("ExecuteCommand() error = %v, want a timeout after 2s", err)
				}
				if elapsed < 2*time.Second {
					t.Errorf("returned after %v, before the 2s timeout", elapsed)
				}
			} else {
				if err != nil {
					t.Fatalf("ExecuteCommand() error = %v", err)
				}
				if result != tt.wantResult {
					t.Errorf("ExecuteCommand() = %q, want %q", result, tt.wantResult)
				}
			}
			if elapsed > tt.maxElapsed {
				t.Errorf("returned after %v, want at most %v", elapsed, tt.maxElapsed)
			}
			if gone := !groupRunning(pgid); gone != tt.groupGone {
				t.Errorf("process group %d gone after the call = %v, want %v", pgid, gone, tt.groupGone)
			}
		})
	}
}

// A command started in one turn is not running when the next turn begins.
func TestExecuteCommand_TurnEndKillsBackgroundJobs(t *testing.T) {
	dir := t.TempDir()
	ctx, processes := turn.Begin(context.Background())

	result, err := NewCommandExecutor(time.Minute).ExecuteCommand(ctx, "echo $$ > pgid; sleep 600 & echo started", dir)
	if err != nil || result != "started" {
		t.Fatalf("ExecuteCommand() = %q, %v; want started", result, err)
	}
	pgid := readProcessGroup(t, filepath.Join(dir, "pgid"))
	if !groupRunning(pgid) {
		t.Fatalf("background sleep of group %d exited before the turn ended", pgid)
	}

	processes.End()
	deadline := time.Now().Add(2 * time.Second)
	for groupRunning(pgid) {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d is still running after the turn ended", pgid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readProcessGroup reads the process group ID a test command wrote, and kills
// the group when the test ends so nothing it started outlives the test.
func readProcessGroup(t *testing.T, path string) int {
	t.Helper()
	pgid := readPid(t, path)
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	return pgid
}

// groupRunning reports whether a live process is left in the group pgid. An
// exited process its parent has not reaped yet counts as gone.
func groupRunning(pgid int) bool {
	if syscall.Kill(-pgid, 0) != nil {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		// No procfs (macOS): the signal check is all there is.
		return true
	}
	for _, entry := range entries {
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// After the parenthesised command name: state, ppid, pgrp.
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) > 2 && fields[2] == strconv.Itoa(pgid) && fields[0] != "Z" {
			return true
		}
	}
	return false
}
