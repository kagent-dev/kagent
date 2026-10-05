// Package utils provides small, shared operating-system helpers for Harness runtimes.
package utils

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
func ReplacePrivateFile(path string, contents []byte) error {
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return replaceFile(path, contents, 0o600, -1)
}

// EnsureGroupReadableDir creates path when necessary, rejects symlinks and
// non-directories, and leaves it owned by the caller and group gid with mode
// 0750: members of gid can list and traverse it but not change its entries.
// Missing parents are created 0755 so that the group can reach it.
func EnsureGroupReadableDir(path string, gid int) error {
	if err := mkdirParents(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create parents of group-readable directory %q: %w", path, err)
	}
	if err := os.Mkdir(path, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create group-readable directory %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect group-readable directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("group-readable path %q is not a directory", path)
	}
	if err := os.Lchown(path, os.Geteuid(), gid); err != nil {
		return fmt.Errorf("set group of %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		return fmt.Errorf("secure group-readable directory %q: %w", path, err)
	}
	return nil
}

// mkdirParents creates path and its missing parents with mode 0755 whatever
// the umask, leaving existing directories as they are.
func mkdirParents(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%q is not a directory", path)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := mkdirParents(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.Chmod(path, 0o755)
}

// ReplaceGroupReadableFile atomically replaces path with contents that the
// caller owns and group gid can read but not write, in a directory prepared by
// EnsureGroupReadableDir, so members of gid can neither edit nor replace it.
func ReplaceGroupReadableFile(path string, contents []byte, gid int) error {
	if err := EnsureGroupReadableDir(filepath.Dir(path), gid); err != nil {
		return err
	}
	return replaceFile(path, contents, 0o640, gid)
}

// replaceFile writes contents to a temporary file beside path with mode and,
// when gid is not negative, group gid, then renames it over path.
func replaceFile(path string, contents []byte, mode os.FileMode, gid int) (returnErr error) {
	directory := filepath.Dir(path)
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
	if gid >= 0 {
		if err := temporary.Chown(os.Geteuid(), gid); err != nil {
			return fmt.Errorf("set group of temporary file for %q: %w", path, err)
		}
	}
	if err := temporary.Chmod(mode); err != nil {
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
		return fmt.Errorf("replace file %q: %w", path, err)
	}
	renamed = true
	return nil
}

// ReclaimTree gives root and everything under it back to the calling root
// process, directory before contents, so each directory is the caller's to
// list by the time the walk reads it. Directories also regain owner rwx. It
// needs CAP_CHOWN but neither CAP_DAC_OVERRIDE nor CAP_DAC_READ_SEARCH, and
// follows no symlink. A missing root is left alone.
func ReclaimTree(root string) error {
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := os.Lchown(path, 0, 0); err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o700 == 0o700 {
			return nil
		}
		return os.Chmod(path, info.Mode().Perm()|0o700)
	})
}

// ChownTree hands root and everything under it to uid:gid without following
// symlinks, contents before their directory, so the caller never needs to read
// a directory it has already handed over. A regular file with more than one
// link stays the caller's: another of its names may be a file the caller
// keeps. A missing root is left alone.
func ChownTree(root string, uid, gid int) error {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if linkCount(info) > 1 {
				return nil
			}
		}
		paths = append(paths, path)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, path := range slices.Backward(paths) {
		if err := os.Lchown(path, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// ShareTreeReadOnly leaves root and everything under it owned by the calling
// root process and readable by group gid only: directories 0750, regular files
// 0640, or 0750 when their owner may execute them. Symlinks keep their target
// and only change owner; no symlink is followed. Members of gid can read the
// tree but not change it.
func ShareTreeReadOnly(root string, gid int) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := os.Lchown(path, 0, gid); err != nil {
			return err
		}
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			return nil
		case entry.IsDir():
			return os.Chmod(path, 0o750)
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0o100 != 0 {
				return os.Chmod(path, 0o750)
			}
			return os.Chmod(path, 0o640)
		default:
			return fmt.Errorf("%s is neither a directory, a regular file nor a symlink", path)
		}
	})
}

// SecureTree makes root and everything under it the calling root process's
// alone, for state another user may have owned: it removes every symlink,
// takes ownership and clears group and other write. A missing root is left
// alone; a root that is a symlink is removed.
func SecureTree(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(root)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return os.Remove(path)
		}
		if err := os.Lchown(path, 0, 0); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() &^ 0o022
		if entry.IsDir() {
			mode |= 0o700
		}
		return os.Chmod(path, mode)
	})
}
