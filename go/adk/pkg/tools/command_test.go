package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		fail          bool
	}{
		{"warning", "printf 'out\\n'; printf 'WARNING: diagnostic\\n' >&2", false},
		{"failure", "printf 'out\\n'; printf 'WARNING: diagnostic\\n' >&2; exit 7", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := executeCommand(t.Context(), tc.command, commandConfig{directory: t.TempDir()})
			require.Contains(t, output, "out")
			require.Contains(t, output, "WARNING: diagnostic")
			if tc.fail {
				require.ErrorContains(t, err, "exit code 7")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCommandOutputIsBoundedAndDrained(t *testing.T) {
	directory := t.TempDir()
	output, err := executeCommand(t.Context(), "head -c 1048576 /dev/zero; echo finished > finished", commandConfig{directory: directory})
	require.NoError(t, err)
	require.True(t, strings.Contains(output, "[output truncated"), "missing truncation marker; bytes=%d", len(output))
	require.LessOrEqual(t, len(output), maxCommandOutput+len(commandTruncationMarker))
	content, err := os.ReadFile(filepath.Join(directory, "finished"))
	require.NoError(t, err)
	require.Equal(t, "finished\n", string(content))
}

func TestCommandTimeoutAndCancellationStopDescendants(t *testing.T) {
	for _, cancelCall := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "cancel"}[cancelCall], func(t *testing.T) {
			directory := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg := commandConfig{directory: directory, timeout: 50 * time.Millisecond}
			if cancelCall {
				cfg.timeout = time.Minute
				timer := time.AfterFunc(50*time.Millisecond, cancel)
				defer timer.Stop()
			}
			started := time.Now()
			output, err := executeCommand(ctx, "echo started; (sleep 0.3; echo leaked > leaked) & wait", cfg)
			require.Error(t, err)
			require.Contains(t, output, "started")
			if cancelCall {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			require.Less(t, time.Since(started), time.Second)
			time.Sleep(350 * time.Millisecond)
			_, statErr := os.Stat(filepath.Join(directory, "leaked"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestCommandDoesNotWaitForBackgroundPipe(t *testing.T) {
	started := time.Now()
	output, err := executeCommand(t.Context(), "sleep 60 & echo done", commandConfig{directory: t.TempDir()})
	// WaitDelay can report that a child held an output pipe open; it must never hang.
	require.True(t, err == nil || strings.Contains(err.Error(), "WaitDelay"), "%v", err)
	require.Contains(t, output, "done")
	require.Less(t, time.Since(started), 3*time.Second)
}

func TestCommandEnvironment(t *testing.T) {
	cfg := commandConfig{directory: "/data/workspace", skillsDirectory: "/skills/demo", bashVenvPath: "/opt/tools"}
	env := commandEnvironment([]string{"PATH=/usr/bin", "PYTHONPATH=/existing", "OPENAI_API_KEY=egress-placeholder"}, cfg)
	require.True(t, slices.Contains(env, "OPENAI_API_KEY=egress-placeholder"))
	require.Contains(t, env, "PYTHONPATH=/data/workspace:/skills/demo:/existing")
	require.Contains(t, env, "PATH=/opt/tools/bin:/usr/bin")
	require.Contains(t, env, "VIRTUAL_ENV=/opt/tools")
}

func TestCommandHonorsVenvAndSkillImports(t *testing.T) {
	root := t.TempDir()
	venv := filepath.Join(root, "venv")
	require.NoError(t, os.MkdirAll(filepath.Join(venv, "bin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(venv, "bin", "kagent-test"), []byte("#!/bin/sh\nprintf venv"), 0755))
	output, err := executeCommand(t.Context(), "kagent-test; printf '\\n%s\\n%s' \"$VIRTUAL_ENV\" \"$PYTHONPATH\"", commandConfig{directory: root, bashVenvPath: venv, skillsDirectory: "/skills"})
	require.NoError(t, err)
	require.Contains(t, output, "venv\n"+venv)
	require.Contains(t, output, root+":/skills")
}

func TestCommandErrorRetainsCause(t *testing.T) {
	_, err := executeCommand(t.Context(), "true", commandConfig{directory: filepath.Join(t.TempDir(), "missing")})
	require.True(t, errors.Is(err, os.ErrNotExist), "%v", err)
}

func TestExecuteCommand(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	tests := []struct {
		name       string
		command    string
		workingDir string
		wantErr    bool
		checkFn    func(t *testing.T, result string)
	}{
		{
			name:       "simple echo command",
			command:    "echo 'hello world'",
			workingDir: tmpDir,
			checkFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "hello world") {
					t.Errorf("Expected 'hello world' in result, got %q", result)
				}
			},
		},
		{
			name:       "command with output",
			command:    "echo -n 'test'",
			workingDir: tmpDir,
			checkFn: func(t *testing.T, result string) {
				if result != "test" {
					t.Errorf("Expected 'test', got %q", result)
				}
			},
		},
		{
			name:       "command that creates file",
			command:    "echo 'content' > test.txt",
			workingDir: tmpDir,
			checkFn: func(t *testing.T, result string) {
				// Check if file was created
				filePath := filepath.Join(tmpDir, "test.txt")
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("Failed to read created file: %v", err)
				}
				if !strings.Contains(string(content), "content") {
					t.Errorf("Expected 'content' in file, got %q", string(content))
				}
			},
		},
		{
			name:       "failing command",
			command:    "false",
			workingDir: tmpDir,
			wantErr:    true,
		},
		{
			name:       "command with stderr",
			command:    "echo 'error' >&2 && echo 'output'",
			workingDir: tmpDir,
			checkFn: func(t *testing.T, result string) {
				// Should include both stdout and stderr
				if !strings.Contains(result, "output") {
					t.Error("Expected 'output' in result")
				}
				// stderr should be included (non-WARNING stderr is appended)
				if !strings.Contains(result, "error") {
					t.Error("Expected 'error' (from stderr) in result")
				}
			},
		},
		{
			name:       "empty output command",
			command:    "true",
			workingDir: tmpDir,
			checkFn: func(t *testing.T, result string) {
				// Empty output should return success message
				if result == "" {
					t.Error("Expected success message for empty output")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executeCommand(ctx, tt.command, commandConfig{directory: tt.workingDir})
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ExecuteCommand() error = %v", err)
			}

			if tt.checkFn != nil {
				tt.checkFn(t, result)
			}
		})
	}
}
