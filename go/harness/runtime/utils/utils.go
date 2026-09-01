// Package utils provides small, shared operating-system helpers for Harness runtimes.
package utils

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// BoundedBuffer retains a limited prefix of process output while reporting
// every write as consumed, so reaching the diagnostic limit never disrupts
// the child process.
type BoundedBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

// NewBoundedBuffer returns an empty buffer that retains the first limit bytes.
func NewBoundedBuffer(limit int) *BoundedBuffer {
	return &BoundedBuffer{limit: max(limit, 0)}
}

// Write implements io.Writer.
func (b *BoundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(len(data), remaining)]...)
	}
	return len(data), nil
}

// String returns a copy of the retained prefix as a string.
func (b *BoundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// EnsurePrivateDir creates path when necessary, rejects symlinks and
// non-directories, and enforces owner-only permissions.
func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private directory %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private path %q is not a directory", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure private directory %q: %w", path, err)
	}
	return nil
}

// ReplacePrivateFile atomically replaces path with owner-only contents. The
// temporary file is created beside path so the rename stays on one filesystem.
func ReplacePrivateFile(path string, contents []byte) (returnErr error) {
	directory := filepath.Dir(path)
	if err := EnsurePrivateDir(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	closed, renamed := false, false
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, temporary.Close())
		}
		if !renamed {
			if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				returnErr = errors.Join(returnErr, fmt.Errorf("remove temporary file %q: %w", temporaryPath, err))
			}
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary file for %q: %w", path, err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace private file %q: %w", path, err)
	}
	renamed = true
	return nil
}

// ConfigureProcessGroup prepares command for process-group signaling when the
// platform supports it.
func ConfigureProcessGroup(command *exec.Cmd) {
	configureProcessGroup(command)
}

// InterruptProcessGroup asks the process group, or platform fallback process,
// to stop gracefully.
func InterruptProcessGroup(process *os.Process) error {
	return interruptProcessGroup(process)
}

// TerminateProcessGroup requests termination of the process group or platform
// fallback process.
func TerminateProcessGroup(process *os.Process) error {
	return terminateProcessGroup(process)
}

// KillProcessGroup forcibly stops the process group or platform fallback process.
func KillProcessGroup(process *os.Process) error {
	return killProcessGroup(process)
}
