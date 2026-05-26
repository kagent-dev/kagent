package skillsinit

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// copyTree mirrors `cp -rL src/. dst/`: it dereferences symlinks (a symlink to
// a file becomes a regular file in dst; a symlink to a directory is walked).
// We follow links because the old script used `cp -rL`, matching git's
// in-repo symlinks pointing back into the same checkout.
//
// dst must already exist.
func copyTree(src, dst string) error {
	rootInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("copyTree source %q is not a directory", src)
	}

	return walkFollow(src, "", func(rel string, info fs.FileInfo, openFile func() (io.ReadCloser, error)) error {
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			in, err := openFile()
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, in)
			return err
		default:
			// Sockets, devices, fifos in a skill checkout don't make sense;
			// skip them rather than failing.
			return nil
		}
	})
}

// walkFollow walks src dereferencing symlinks. rel is the path relative to
// src ("" for the root). The walker invokes fn with a lazily-opened reader
// for regular files.
func walkFollow(src, rel string, fn func(rel string, info fs.FileInfo, openFile func() (io.ReadCloser, error)) error) error {
	full := filepath.Join(src, rel)
	info, err := os.Stat(full) // Stat follows symlinks
	if err != nil {
		return err
	}
	if rel != "" {
		if err := fn(rel, info, func() (io.ReadCloser, error) { return os.Open(full) }); err != nil {
			return err
		}
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := walkFollow(src, filepath.Join(rel, e.Name()), fn); err != nil {
			return err
		}
	}
	return nil
}
