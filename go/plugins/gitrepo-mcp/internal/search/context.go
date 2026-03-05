package search

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// Context holds lines before and after a code chunk for display purposes.
type Context struct {
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// ExtractContext reads surrounding lines from a file on disk.
// lineStart and lineEnd are 1-indexed. contextLines is the number of lines
// to include before and after the chunk.
func ExtractContext(repoPath, filePath string, lineStart, lineEnd, contextLines int) (*Context, error) {
	if contextLines <= 0 {
		return nil, nil
	}

	fullPath := filepath.Join(repoPath, filePath)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", fullPath, err)
	}
	defer f.Close()

	// Read all lines
	var lines []string
	scanner := bufio.NewScanner(f)
	// Increase buffer size for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", fullPath, err)
	}

	totalLines := len(lines)
	if totalLines == 0 {
		return nil, nil
	}

	// Convert to 0-indexed
	startIdx := lineStart - 1
	endIdx := lineEnd - 1

	// Clamp indices
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx >= totalLines {
		endIdx = totalLines - 1
	}

	// Extract before lines
	beforeStart := startIdx - contextLines
	if beforeStart < 0 {
		beforeStart = 0
	}
	var before []string
	if beforeStart < startIdx {
		before = make([]string, startIdx-beforeStart)
		copy(before, lines[beforeStart:startIdx])
	}

	// Extract after lines
	afterEnd := endIdx + 1 + contextLines
	if afterEnd > totalLines {
		afterEnd = totalLines
	}
	var after []string
	if endIdx+1 < afterEnd {
		after = make([]string, afterEnd-endIdx-1)
		copy(after, lines[endIdx+1:afterEnd])
	}

	if len(before) == 0 && len(after) == 0 {
		return nil, nil
	}

	return &Context{Before: before, After: after}, nil
}
