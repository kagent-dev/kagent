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

func installFakeSRT(t *testing.T) string {
	t.Helper()

	tmpDir := createTempDir(t)
	scriptPath := filepath.Join(tmpDir, "srt")
	script := "#!/bin/sh\nif [ \"$1\" = \"--settings\" ]; then\n  shift 2\nfi\nexec \"$@\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write fake srt: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, "srt-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"network":{"allowedDomains":[],"deniedDomains":[]},"filesystem":{"denyRead":[],"allowWrite":[".","/tmp"],"denyWrite":[]}}`), 0644); err != nil {
		t.Fatalf("Failed to write fake srt settings: %v", err)
	}

	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(srtSettingsPathEnv, settingsPath)
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
				lines := strings.SplitSeq(result, "\n")
				for line := range lines {
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

func TestListDirContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, "a-subdir"), 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	t.Run("lists files and directories", func(t *testing.T) {
		result, err := ListDirContent(tmpDir)
		if err != nil {
			t.Fatalf("ListDirContent() error = %v", err)
		}
		if !strings.Contains(result, "a-subdir/") {
			t.Errorf("expected directory entry with trailing slash, got %q", result)
		}
		if !strings.Contains(result, "b.txt\t5") {
			t.Errorf("expected file entry with size, got %q", result)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "a-subdir")
		result, err := ListDirContent(emptyDir)
		if err != nil {
			t.Fatalf("ListDirContent() error = %v", err)
		}
		if result != "Directory is empty." {
			t.Errorf("expected empty directory message, got %q", result)
		}
	})

	t.Run("nonexistent path", func(t *testing.T) {
		if _, err := ListDirContent(filepath.Join(tmpDir, "does-not-exist")); err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})
}

func TestGrepContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nFOO bar\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("another foo line\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	t.Run("matches within a single file", func(t *testing.T) {
		result, err := GrepContent(filepath.Join(tmpDir, "a.txt"), "hello", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "a.txt:1:hello world") {
			t.Errorf("expected match with path:line:content, got %q", result)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		result, err := GrepContent(filepath.Join(tmpDir, "a.txt"), "nope", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if result != "no matches found" {
			t.Errorf("expected no matches message, got %q", result)
		}
	})

	t.Run("ignore case", func(t *testing.T) {
		result, err := GrepContent(filepath.Join(tmpDir, "a.txt"), "foo", false, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "FOO bar") {
			t.Errorf("expected case-insensitive match, got %q", result)
		}
	})

	t.Run("directory requires recursive", func(t *testing.T) {
		if _, err := GrepContent(tmpDir, "foo", false, false); err == nil {
			t.Fatal("expected error when searching a directory without recursive=true")
		}
	})

	t.Run("recursive searches subdirectories", func(t *testing.T) {
		result, err := GrepContent(tmpDir, "foo", true, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "b.txt:1:another foo line") {
			t.Errorf("expected match from subdirectory, got %q", result)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		if _, err := GrepContent(filepath.Join(tmpDir, "a.txt"), "(", false, false); err == nil {
			t.Fatal("expected error for invalid regex pattern")
		}
	})

	t.Run("recursive search skips symlinks that escape the root", func(t *testing.T) {
		outsideDir := createTempDir(t)
		defer os.RemoveAll(outsideDir)
		secretPath := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(secretPath, []byte("top secret foo\n"), 0644); err != nil {
			t.Fatalf("Failed to write outside file: %v", err)
		}

		linkPath := filepath.Join(subDir, "escape.txt")
		if err := os.Symlink(secretPath, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		result, err := GrepContent(tmpDir, "foo", true, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if strings.Contains(result, "top secret") {
			t.Errorf("expected symlinked file outside root to be skipped, got %q", result)
		}
	})

	t.Run("recursive search does not abort on an in-bounds directory symlink", func(t *testing.T) {
		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		if err := os.WriteFile(filepath.Join(walkDir, "aaa_first.txt"), []byte("foo one\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		realSub := filepath.Join(walkDir, "real_sub")
		if err := os.Mkdir(realSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}

		// A symlink to an in-bounds directory, lexically sorted between the
		// two files below, so an incorrect abort partway through the walk
		// would silently drop "zzz_sub"'s match.
		linkPath := filepath.Join(walkDir, "mmm_link")
		if err := os.Symlink(realSub, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		zzzSub := filepath.Join(walkDir, "zzz_sub")
		if err := os.Mkdir(zzzSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(zzzSub, "zzz_last.txt"), []byte("foo two\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		result, err := GrepContent(walkDir, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "aaa_first.txt:1:foo one") {
			t.Errorf("expected match before the symlink, got %q", result)
		}
		if !strings.Contains(result, "zzz_last.txt:1:foo two") {
			t.Errorf("expected match after the symlink (walk must not abort on it), got %q", result)
		}
	})
}

func TestExecuteCommand(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)
	defer os.RemoveAll(installFakeSRT(t))

	ctx := context.Background()
	executor, err := NewCommandExecutorFromEnv()
	if err != nil {
		t.Fatalf("NewCommandExecutorFromEnv() error = %v", err)
	}

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
			result, err := executor.ExecuteCommand(ctx, tt.command, tt.workingDir)
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

func TestExecuteCommand_RequiresMountedSRTSettings(t *testing.T) {
	t.Setenv(srtSettingsPathEnv, "")

	_, err := NewCommandExecutorFromEnv()
	if err == nil {
		t.Fatal("expected error when SRT settings path is missing")
	}
	if !strings.Contains(err.Error(), srtSettingsPathEnv+" is not set") {
		t.Fatalf("unexpected error: %v", err)
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
	defer os.RemoveAll(installFakeSRT(t))

	ctx := context.Background()
	executor, err := NewCommandExecutorFromEnv()
	if err != nil {
		t.Fatalf("NewCommandExecutorFromEnv() error = %v", err)
	}

	// Test timeout for long-running command
	// The timeout is 30 seconds for non-python commands
	// Use a command that will definitely exceed the timeout
	// Use sleep 31 to ensure it exceeds 30s timeout but completes faster for testing
	command := "sleep 31" // This should timeout after 30 seconds

	start := time.Now()
	result, err := executor.ExecuteCommand(ctx, command, tmpDir)
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
