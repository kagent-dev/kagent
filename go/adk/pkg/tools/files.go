package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxLineRunes         = 2000
	maxLineBytes         = 1024 * 1024
	maxFileOutputBytes   = 64 * 1024
	defaultReadLines     = 2000
	fileTruncationMarker = "[output truncated; use read_file offset/limit or a narrower search]"
)

// appendResultLine preserves complete lines within the result budget.
func appendResultLine(result *strings.Builder, line string) bool {
	if result.Len()+len(line)+1 > maxFileOutputBytes {
		result.WriteString(fileTruncationMarker)
		return false
	}
	result.WriteString(line)
	result.WriteByte('\n')
	return true
}

// scanFileLines rejects special files and bounds line buffers before visiting.
func scanFileLines(ctx context.Context, path string, visit func(lineNum int, line string) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, maxLineBytes)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !visit(lineNum, scanner.Text()) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read %q: %w", path, err)
	}
	return nil
}

// truncateRunes truncates by code point without splitting UTF-8 sequences.
func truncateRunes(s string, maxRunes int) string {
	count := 0
	for i := range s {
		if count == maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// readFileContent returns a bounded page with source line numbers.
func readFileContent(ctx context.Context, path string, offset, limit int) (string, error) {
	var result strings.Builder
	start := max(offset, 1)
	lineLimit := limit
	if lineLimit <= 0 {
		lineLimit = defaultReadLines
	}
	count := 0
	err := scanFileLines(ctx, path, func(lineNum int, line string) bool {
		if lineNum < start {
			return true
		}
		count++
		if !appendResultLine(&result, fmt.Sprintf("%6d|%s", lineNum, truncateRunes(line, maxLineRunes))) {
			return false
		}
		if count == lineLimit {
			if limit <= 0 {
				fmt.Fprintf(&result, "[read limited to %d lines; continue with offset=%d]", lineLimit, lineNum+1)
			}
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if result.Len() == 0 {
		return "File is empty.", nil
	}
	return strings.TrimSuffix(result.String(), "\n"), nil
}

// writeFileContent writes content to a file.
func writeFileContent(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// editFileContent performs an exact string replacement in a file.
func editFileContent(path string, oldString, newString string, replaceAll bool) error {
	if oldString == "" {
		return fmt.Errorf("old_string cannot be empty")
	}
	if oldString == newString {
		return fmt.Errorf("old_string and new_string must be different")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, oldString) {
		return fmt.Errorf("old_string not found in %s", path)
	}

	count := strings.Count(contentStr, oldString)
	if !replaceAll && count > 1 {
		return fmt.Errorf("old_string appears %d times in %s. Provide more context or set replace_all=true", count, path)
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(contentStr, oldString, newString)
	} else {
		newContent = strings.Replace(contentStr, oldString, newString, 1)
	}

	return os.WriteFile(path, []byte(newContent), 0644)
}

// listDirContent streams directory entries without loading the whole listing.
func listDirContent(ctx context.Context, path string) (string, error) {
	directory, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open directory %q: %w", path, err)
	}
	defer directory.Close()
	var result strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		entries, readErr := directory.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", fmt.Errorf("failed to read directory %q: %w", path, readErr)
		}
		for _, entry := range entries {
			line := entry.Name()
			info, statErr := entry.Info()
			if entry.Type()&fs.ModeSymlink != 0 {
				info, statErr = os.Stat(filepath.Join(path, entry.Name()))
			}
			if statErr == nil {
				if info.IsDir() {
					line += "/"
				} else {
					line += fmt.Sprintf("\t%d", info.Size())
				}
			}
			if !appendResultLine(&result, line) {
				return result.String(), nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if result.Len() == 0 {
		return "Directory is empty.", nil
	}
	return strings.TrimSuffix(result.String(), "\n"), nil
}
