package skillsinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CloneGit fetches a single git ref into ref.Dest. All user-controlled
// strings (URL, Ref, SubPath) are passed to git as separate argv entries via
// exec.Command — they never pass through a shell, so metacharacters in any of
// them are inert.
//
// When ref.Full is true we do a full clone then `git checkout <sha>`,
// because shallow `--branch` does not accept commit SHAs. When false we use a
// depth-1 branch/tag clone.
//
// SubPath, if set, rewrites the destination so the final layout matches the
// requested in-repo subdirectory.
func CloneGit(ref GitRef) error {
	if ref.Full {
		if err := runGit("clone", "--", ref.URL, ref.Dest); err != nil {
			return err
		}
		// `--` separator prevents a ref starting with `-` from being parsed
		// as a flag. Refs are already validated upstream as 40-char hex when
		// Full is true, but defense in depth costs nothing.
		if err := runGitIn(ref.Dest, "checkout", "--", ref.Ref); err != nil {
			return err
		}
	} else {
		if err := runGit("clone", "--depth", "1", "--branch", ref.Ref, "--", ref.URL, ref.Dest); err != nil {
			return err
		}
	}

	if ref.SubPath != "" {
		if err := applySubPath(ref.Dest, ref.SubPath); err != nil {
			return fmt.Errorf("apply subPath %q: %w", ref.SubPath, err)
		}
	}
	return nil
}

func runGit(args ...string) error {
	return runGitIn("", args...)
}

func runGitIn(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

// applySubPath replaces dest with the contents of dest/subPath. The result is
// that dest contains only the requested subdirectory. We materialize the
// content into a sibling tmp dir under the same parent so the final rename is
// atomic on the same filesystem.
func applySubPath(dest, subPath string) error {
	// Defense in depth: filepath.IsLocal rejects absolute paths, ".."
	// segments, and reserved names — exactly the set we want to refuse.
	if !filepath.IsLocal(subPath) {
		return fmt.Errorf("invalid subPath %q", subPath)
	}
	clean := filepath.Clean(subPath)
	src := filepath.Join(dest, clean)
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat subPath: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("subPath %q is not a directory", subPath)
	}

	parent := filepath.Dir(dest)
	tmp, err := os.MkdirTemp(parent, ".skill-subpath-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	// Best-effort cleanup if we fail before the rename.
	cleanupTmp := tmp
	defer func() {
		if cleanupTmp != "" {
			os.RemoveAll(cleanupTmp)
		}
	}()

	// cp -rL: follow symlinks (matches the original behavior); both paths
	// are constructed by us, never user-supplied, and are passed as argv —
	// no shell, no metacharacter risk. Trailing "/." copies *contents* of
	// src into tmp, not src itself.
	cp := exec.Command("cp", "-rL", "--", src+"/.", tmp)
	cp.Stdout = os.Stdout
	cp.Stderr = os.Stderr
	if err := cp.Run(); err != nil {
		return fmt.Errorf("cp subPath contents: %w", err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	cleanupTmp = ""
	return nil
}
