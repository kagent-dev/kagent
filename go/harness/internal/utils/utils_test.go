package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedBufferConsumesWritesAndBoundsRetainedOutput(t *testing.T) {
	buffer := NewBoundedBuffer(5)
	for _, value := range []string{"abc", "def", "ghi"} {
		written, err := buffer.Write([]byte(value))
		if err != nil || written != len(value) {
			t.Fatalf("Write(%q) = %d, %v", value, written, err)
		}
	}
	if got := buffer.String(); got != "abcde" {
		t.Fatalf("String() = %q, want %q", got, "abcde")
	}
}

func TestReplacePrivateFileAtomicallyReplacesPrivateContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	for _, contents := range []string{"before", "after"} {
		if err := ReplacePrivateFile(path, []byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "after" {
		t.Fatalf("contents = %q, want %q", contents, "after")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions = %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions = %v, %v", info, err)
	}
}

func TestChownTreeWalksWithoutFollowingSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := ChownTree(root, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("ChownTree() = %v", err)
	}
	if err := ChownTree(filepath.Join(root, "missing"), os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("a missing root is left alone: %v", err)
	}
}
