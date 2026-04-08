package testutil

import (
	"testing"

	"github.com/spf13/afero"
)

// NewMemFS creates an in-memory filesystem for testing.
// This allows tests to run without touching the actual filesystem.
func NewMemFS() afero.Fs {
	return afero.NewMemMapFs()
}

// CreateTestFile writes a file to the filesystem for testing.
func CreateTestFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()

	err := afero.WriteFile(fs, path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file %s: %v", path, err)
	}
}

// CreateTestDir creates a directory in the filesystem for testing.
func CreateTestDir(t *testing.T, fs afero.Fs, path string) {
	t.Helper()

	err := fs.MkdirAll(path, 0755)
	if err != nil {
		t.Fatalf("failed to create test directory %s: %v", path, err)
	}
}

// ReadTestFile reads a file from the filesystem for testing assertions.
func ReadTestFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()

	content, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatalf("failed to read test file %s: %v", path, err)
	}

	return string(content)
}

// FileExists checks if a file exists in the filesystem.
func FileExists(t *testing.T, fs afero.Fs, path string) bool {
	t.Helper()

	exists, err := afero.Exists(fs, path)
	if err != nil {
		t.Fatalf("failed to check if file exists %s: %v", path, err)
	}

	return exists
}
