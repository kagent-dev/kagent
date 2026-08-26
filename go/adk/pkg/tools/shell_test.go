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
	"unicode/utf8"
)

func createTempDir(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "skills-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return tmpDir
}

func TestGetSessionPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()

	sessionPath, err := GetSessionPath("test-session", skillsDir)
	if err != nil {
		t.Fatalf("GetSessionPath() error = %v", err)
	}
	target, err := os.Readlink(filepath.Join(sessionPath, "skills"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != skillsDir {
		t.Fatalf("skills link = %q, want %q", target, skillsDir)
	}
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

// TestReadFileContent_LongLineIsTruncatedNotFatal pins the buffer that
// scanFileLines sets. bufio.Scanner's default cap is 64KB, and exceeding it
// fails the *whole* scan -- so before the shared reader existed, one long line
// (a minified bundle, a single-line JSON blob) made every other line in the
// file unreadable too, even though grep_file handled the same file fine and
// read_file's own tool description promises such lines are truncated.
func TestReadFileContent_LongLineIsTruncatedNotFatal(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	// Well past bufio's 64KB default, well under scanFileLines' maxLineBytes.
	longLine := strings.Repeat("a", 100_000)
	path := filepath.Join(tmpDir, "bundle.min.js")
	if err := os.WriteFile(path, []byte(longLine+"\nsecond line\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result, err := ReadFileContent(path, 0, 0)
	if err != nil {
		t.Fatalf("ReadFileContent() error = %v, want the long line truncated instead", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), result)
	}
	// Six-column line number, "|", then the truncated body and the ellipsis.
	body := strings.TrimSuffix(strings.SplitN(lines[0], "|", 2)[1], "...")
	if n := utf8.RuneCountInString(body); n != maxLineRunes {
		t.Errorf("expected the long line truncated to %d runes, got %d", maxLineRunes, n)
	}
	if !strings.HasSuffix(lines[0], "...") {
		t.Error("expected the truncated line to end with '...'")
	}
	// The whole point: the rest of the file survives.
	if !strings.Contains(lines[1], "second line") {
		t.Errorf("expected the line after the long one to still be read, got %q", lines[1])
	}
}

// TestReadFileContent_LineOverMaxBytesErrors documents the deliberate residual
// limit: maxLineBytes exists so a sandboxed runtime can't be made to buffer an
// arbitrarily long line, so a line past it still fails rather than truncating.
func TestReadFileContent_LineOverMaxBytesErrors(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "huge.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", maxLineBytes+1)+"\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if _, err := ReadFileContent(path, 0, 0); err == nil {
		t.Fatal("expected a line over maxLineBytes to error, got nil")
	}
}

func TestReadFileContent_FIFOReturnsErrorInsteadOfHanging(t *testing.T) {
	fifoDir := createTempDir(t)
	defer os.RemoveAll(fifoDir)

	fifoPath := filepath.Join(fifoDir, "pipe")
	if err := syscall.Mkfifo(fifoPath, 0644); err != nil {
		t.Skipf("FIFOs not supported: %v", err)
	}

	// Opening a FIFO with no writer connected blocks forever, and nothing on
	// this path has a timeout -- so run it off-goroutine and fail on the
	// timeout rather than hanging the whole test binary.
	done := make(chan struct{})
	var err error
	go func() {
		_, err = ReadFileContent(fifoPath, 0, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFileContent hung opening a FIFO instead of erroring")
	}

	if err == nil {
		t.Fatal("expected an error for a non-regular file target, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected a not-a-regular-file error, got %v", err)
	}
}

func TestTruncateRunes(t *testing.T) {
	t.Run("truncates on a rune boundary and stays valid UTF-8", func(t *testing.T) {
		// 3000 three-byte runes: byte-slicing at 2000 would land mid-rune.
		long := strings.Repeat("世", 3000)

		got := truncateRunes(long, maxLineRunes)

		if !utf8.ValidString(got) {
			t.Error("expected truncated output to be valid UTF-8")
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Error("expected no U+FFFD replacement char from a split rune")
		}
		body := strings.TrimSuffix(got, "...")
		if n := utf8.RuneCountInString(body); n != maxLineRunes {
			t.Errorf("expected exactly %d runes before the ellipsis, got %d", maxLineRunes, n)
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("expected truncated output to end with '...', got %q", got)
		}
	})

	t.Run("leaves a short string untouched", func(t *testing.T) {
		if got := truncateRunes("héllo", maxLineRunes); got != "héllo" {
			t.Errorf("expected input returned unchanged, got %q", got)
		}
	})

	t.Run("counts characters not bytes, matching the Python runtime", func(t *testing.T) {
		// 2000 multi-byte runes is 6000 bytes -- well over a byte-based
		// limit, but exactly at the rune limit, so it must NOT truncate.
		exact := strings.Repeat("世", maxLineRunes)
		if got := truncateRunes(exact, maxLineRunes); got != exact {
			t.Errorf("expected a string of exactly %d runes to be left alone, got %d runes",
				maxLineRunes, utf8.RuneCountInString(got))
		}
	})
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

	t.Run("symlink to a directory is listed as a directory", func(t *testing.T) {
		realDir := filepath.Join(tmpDir, "a-subdir")
		linkPath := filepath.Join(tmpDir, "dir-link")
		if err := os.Symlink(realDir, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		result, err := ListDirContent(tmpDir)
		if err != nil {
			t.Fatalf("ListDirContent() error = %v", err)
		}
		if !strings.Contains(result, "dir-link/") {
			t.Errorf("expected symlinked directory to be listed with a trailing slash, got %q", result)
		}
		if strings.Contains(result, "dir-link\t") {
			t.Errorf("expected symlinked directory not to be listed as a file, got %q", result)
		}
	})

	t.Run("symlink to a file reports the target's size, not the link's", func(t *testing.T) {
		linkDir := createTempDir(t)
		defer os.RemoveAll(linkDir)

		// The target's contents must be longer than the symlink's own
		// "size" (the byte length of the stored target path) so the two are
		// unambiguous: Lstat would report len(target path), Stat reports 20.
		target := filepath.Join(linkDir, "target.txt")
		contents := strings.Repeat("x", 20)
		if err := os.WriteFile(target, []byte(contents), 0644); err != nil {
			t.Fatalf("Failed to write target file: %v", err)
		}
		linkPath := filepath.Join(linkDir, "file-link")
		if err := os.Symlink(target, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		result, err := ListDirContent(linkDir)
		if err != nil {
			t.Fatalf("ListDirContent() error = %v", err)
		}
		if !strings.Contains(result, fmt.Sprintf("file-link\t%d", len(contents))) {
			t.Errorf("expected symlinked file to report the target's size (%d), got %q", len(contents), result)
		}
		if strings.Contains(result, fmt.Sprintf("file-link\t%d", len(target))) {
			t.Errorf("expected not to report the symlink's own size (%d), got %q", len(target), result)
		}
	})

	t.Run("broken symlink is listed without a size", func(t *testing.T) {
		linkDir := createTempDir(t)
		defer os.RemoveAll(linkDir)

		linkPath := filepath.Join(linkDir, "broken-link")
		if err := os.Symlink(filepath.Join(linkDir, "does-not-exist"), linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		result, err := ListDirContent(linkDir)
		if err != nil {
			t.Fatalf("ListDirContent() error = %v", err)
		}
		// Bare name, no size and no trailing slash -- matching Python's
		// list_dir_content, whose stat() raises on a dangling link.
		if !strings.Contains(result, "broken-link") {
			t.Errorf("expected broken symlink to be listed, got %q", result)
		}
		if strings.Contains(result, "broken-link\t") || strings.Contains(result, "broken-link/") {
			t.Errorf("expected broken symlink to be listed with no size and no trailing slash, got %q", result)
		}
	})
}

func TestExecuteCommand(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	executor := NewCommandExecutor()

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

func TestExecuteCommand_Timeout(t *testing.T) {
	// Skip this test if running in CI or if test timeout is too short
	// This test requires at least 35 seconds to run properly
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	executor := NewCommandExecutor()

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
