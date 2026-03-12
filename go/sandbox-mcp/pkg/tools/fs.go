package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirEntry represents a single directory entry.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "dir"
	Size int64  `json:"size"`
}

// ReadFile reads the content of a file at the given path.
func ReadFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return string(data), nil
}

// WriteFile writes content to a file at the given path, creating parent
// directories as needed.
func WriteFile(path string, content string) error {
	if path == "" {
		return fmt.Errorf("path must not be empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// ListDir lists entries in a directory. If path is empty, it defaults to ".".
func ListDir(path string) ([]DirEntry, error) {
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", path, err)
	}

	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		entryType := "file"
		if e.IsDir() {
			entryType = "dir"
		}

		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}

		result = append(result, DirEntry{
			Name: e.Name(),
			Type: entryType,
			Size: size,
		})
	}

	return result, nil
}
