package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createTempDir(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "skills-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return tmpDir
}

func TestReadFileContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		offset  int
		limit   int
		wantErr bool
		checkFn func(t *testing.T, result string)
	}{
		{
			name:   "read entire file",
			path:   filePath,
			offset: 0,
			limit:  0,
			checkFn: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				if len(lines) != 5 {
					t.Errorf("Expected 5 lines, got %d", len(lines))
				}
				if !strings.Contains(result, "line 1") {
					t.Error("Expected 'line 1' in result")
				}
			},
		},
		{
			name:   "read with offset",
			path:   filePath,
			offset: 3,
			limit:  0,
			checkFn: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				if len(lines) != 3 {
					t.Errorf("Expected 3 lines (from line 3), got %d", len(lines))
				}
				if !strings.Contains(result, "line 3") {
					t.Error("Expected 'line 3' in result")
				}
				if strings.Contains(result, "line 1") {
					t.Error("Should not contain 'line 1' when starting from offset 3")
				}
			},
		},
		{
			name:   "read with limit",
			path:   filePath,
			offset: 0,
			limit:  2,
			checkFn: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				if len(lines) != 2 {
					t.Errorf("Expected 2 lines, got %d", len(lines))
				}
			},
		},
		{
			name:   "read with offset and limit",
			path:   filePath,
			offset: 2,
			limit:  2,
			checkFn: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				if len(lines) != 2 {
					t.Errorf("Expected 2 lines, got %d", len(lines))
				}
				if !strings.Contains(result, "line 2") {
					t.Error("Expected 'line 2' in result")
				}
				if !strings.Contains(result, "line 3") {
					t.Error("Expected 'line 3' in result")
				}
			},
		},
		{
			name:    "file not found",
			path:    filepath.Join(tmpDir, "nonexistent.txt"),
			offset:  0,
			limit:   0,
			wantErr: true,
		},
		{
			name:   "empty file",
			path:   filepath.Join(tmpDir, "empty.txt"),
			offset: 0,
			limit:  0,
			checkFn: func(t *testing.T, result string) {
				if result != "File is empty." {
					t.Errorf("Expected 'File is empty.', got %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "empty file" {
				// Create empty file
				if err := os.WriteFile(tt.path, []byte(""), 0644); err != nil {
					t.Fatalf("Failed to create empty file: %v", err)
				}
			}

			result, err := ReadFileContent(tt.path, tt.offset, tt.limit)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadFileContent() error = %v", err)
			}

			// Check line number format (skip for empty file message)
			if result != "File is empty." {
				lines := strings.Split(result, "\n")
				for _, line := range lines {
					if line != "" && !strings.Contains(line, "|") {
						t.Errorf("Expected line number format (number|content), got %q", line)
					}
				}
			}

			if tt.checkFn != nil {
				tt.checkFn(t, result)
			}
		})
	}
}

func TestWriteFileContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "subdir", "test.txt")
	content := "test content\nline 2"

	err := WriteFileContent(filePath, content)
	if err != nil {
		t.Fatalf("WriteFileContent() error = %v", err)
	}

	// Verify file was created
	readContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(readContent) != content {
		t.Errorf("Expected content %q, got %q", content, string(readContent))
	}
}

func TestEditFileContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")
	initialContent := "line 1\nold text\nline 3\nold text\nline 5"
	if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tests := []struct {
		name       string
		oldString  string
		newString  string
		replaceAll bool
		wantErr    bool
		checkFn    func(t *testing.T, content string)
	}{
		{
			name:       "single replacement",
			oldString:  "old text",
			newString:  "new text",
			replaceAll: false,
			checkFn: func(t *testing.T, content string) {
				count := strings.Count(content, "new text")
				if count != 1 {
					t.Errorf("Expected 1 occurrence of 'new text', got %d", count)
				}
				count = strings.Count(content, "old text")
				if count != 1 {
					t.Errorf("Expected 1 remaining 'old text', got %d", count)
				}
			},
		},
		{
			name:       "replace all",
			oldString:  "old text",
			newString:  "new text",
			replaceAll: true,
			checkFn: func(t *testing.T, content string) {
				count := strings.Count(content, "new text")
				if count != 2 {
					t.Errorf("Expected 2 occurrences of 'new text', got %d", count)
				}
				count = strings.Count(content, "old text")
				if count != 0 {
					t.Errorf("Expected 0 remaining 'old text', got %d", count)
				}
			},
		},
		{
			name:       "old_string not found",
			oldString:  "nonexistent",
			newString:  "new text",
			replaceAll: false,
			wantErr:    true,
		},
		{
			name:       "old_string equals new_string",
			oldString:  "line 1",
			newString:  "line 1",
			replaceAll: false,
			wantErr:    true,
		},
		{
			name:       "multiple occurrences without replace_all",
			oldString:  "line",
			newString:  "LINE",
			replaceAll: false,
			wantErr:    true, // Should error when multiple matches and replaceAll=false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset file content before each test
			if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
				t.Fatalf("Failed to reset file: %v", err)
			}

			err := EditFileContent(filePath, tt.oldString, tt.newString, tt.replaceAll)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("EditFileContent() error = %v", err)
			}

			// Read and verify content
			content, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read edited file: %v", err)
			}

			if tt.checkFn != nil {
				tt.checkFn(t, string(content))
			}
		})
	}
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
			result, err := ExecuteCommand(ctx, tt.command, tt.workingDir)
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

func TestExecuteCommand_Timeout(t *testing.T) {
	// Skip this test if running in CI or if test timeout is too short
	// This test requires at least 35 seconds to run properly
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Test timeout for long-running command
	// The timeout is 30 seconds for non-python commands
	// Use a command that will definitely exceed the timeout
	// Use sleep 31 to ensure it exceeds 30s timeout but completes faster for testing
	command := "sleep 31" // This should timeout after 30 seconds

	start := time.Now()
	result, err := ExecuteCommand(ctx, command, tmpDir)
	elapsed := time.Since(start)

	// When a command times out, ExecuteCommand should return an error
	if err == nil {
		// If no error, the command completed (shouldn't happen with sleep 31)
		// This could happen if the test environment is very slow or timeout isn't working
		t.Errorf("Expected timeout error for sleep 31, but command completed with result: %q (elapsed: %v)", result, elapsed)
		return
	}

	// Verify the error is a timeout error
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Expected timeout error, got: %v (elapsed: %v)", err, elapsed)
		return
	}

	// Verify it actually timed out (should be around 30 seconds, not 31+)
	if elapsed < 25*time.Second {
		t.Errorf("Command should have taken ~30 seconds to timeout, but only took %v", elapsed)
	}
	if elapsed > 35*time.Second {
		t.Logf("Warning: Timeout took longer than expected (%v), but test passed", elapsed)
	}

	// Result should be empty when there's an error
	if result != "" {
		t.Logf("Note: Got non-empty result on timeout: %q", result)
	}
}
