//go:build unix

package utils

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReplaceGroupReadableFileLeavesTheGroupReadOnly(t *testing.T) {
	defer syscall.Umask(syscall.Umask(0o077))
	base := t.TempDir()
	path := filepath.Join(base, "missing", "policy", "settings.json")
	if err := ReplaceGroupReadableFile(path, []byte("{}"), os.Getgid()); err != nil {
		t.Fatalf("ReplaceGroupReadableFile() = %v", err)
	}
	for want, target := range map[os.FileMode]string{0o755: filepath.Join(base, "missing"), 0o750: filepath.Dir(path), 0o640: path} {
		info, err := os.Lstat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", target, got, want)
		}
	}
	if err := ReplaceGroupReadableFile(path, []byte(`{"a":1}`), os.Getgid()); err != nil {
		t.Fatalf("replacing the file again = %v", err)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != `{"a":1}` {
		t.Fatalf("contents = %q, %v", contents, err)
	}
}
