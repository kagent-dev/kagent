package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello sandbox"

	if err := WriteFile(path, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if got != content {
		t.Errorf("ReadFile = %q, want %q", got, content)
	}
}

func TestWriteFileCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.txt")

	if err := WriteFile(path, "nested"); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if got != "nested" {
		t.Errorf("ReadFile = %q, want %q", got, "nested")
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	types := map[string]string{}
	for _, e := range entries {
		types[e.Name] = e.Type
	}

	if types["a.txt"] != "file" {
		t.Errorf("expected a.txt to be file, got %s", types["a.txt"])
	}
	if types["subdir"] != "dir" {
		t.Errorf("expected subdir to be dir, got %s", types["subdir"])
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := ReadFile("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadFileEmpty(t *testing.T) {
	_, err := ReadFile("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}
