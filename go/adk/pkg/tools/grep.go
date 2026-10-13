package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// WithinRoot reports whether resolved (an already symlink-resolved path) is
// root itself or nested under it. Callers are responsible for resolving
// symlinks on both arguments first; this is a pure path-containment check.
//
// It lives here with classifyWalkEntry, its main caller, but is also the
// containment test behind resolveSandboxedPath in skills.go.
func WithinRoot(resolved, root string) bool {
	resolved = filepath.Clean(resolved)
	root = filepath.Clean(root)
	return resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator))
}

type walkEntryAction int

const (
	// walkEntryGrep: a regular, in-bounds file that should be grepped.
	walkEntryGrep walkEntryAction = iota
	// walkEntrySkip: silently excluded by policy, not a read failure -- a
	// directory, a symlinked directory, a non-regular file (FIFO/socket/
	// device), or a symlink whose target escapes the search root.
	walkEntrySkip
	// walkEntryUnreadable: a genuine read/stat failure on this entry.
	walkEntryUnreadable
)

// classifyWalkEntry decides how GrepContent's WalkDir callback should treat
// p, given the resolved search root. It never opens p for reading -- the
// caller is responsible for that once this returns walkEntryGrep, and must
// read the returned resolved path rather than p itself: p is the symlink
// as walked, and re-resolving or reopening it separately from this check
// would reintroduce a TOCTOU window where the symlink's target could change
// between the check and the read.
//
// This narrows that window but doesn't eliminate it: resolved is still a
// path string, so the later os.Open on it is a fresh lookup, not an
// operation on a captured file handle. A path component of resolved could
// still be swapped out between this check and that open. Closing that
// residual race fully would need platform-specific work (e.g. Linux's
// openat2 with RESOLVE_NO_SYMLINKS), which isn't done anywhere else in this
// file either -- this function only guarantees "reads what was just
// verified", not "reads atomically with no concurrent tampering".
func classifyWalkEntry(root, p string, d fs.DirEntry) (walkEntryAction, string) {
	if d.IsDir() {
		return walkEntrySkip, ""
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return walkEntryUnreadable, ""
	}
	fi, statErr := os.Stat(resolved)
	if statErr != nil {
		return walkEntryUnreadable, ""
	}
	if fi.IsDir() {
		// p is a symlink to a directory: WalkDir doesn't recurse into
		// symlinked directories, and grepFile would fail trying to read
		// one as a file, so skip it rather than treating it as an error.
		return walkEntrySkip, ""
	}
	if !fi.Mode().IsRegular() {
		// Skip non-regular files (FIFOs, sockets, devices): opening one
		// for reading can block indefinitely (e.g. a FIFO with no writer
		// connected), and grep has no business reading them.
		return walkEntrySkip, ""
	}
	if !WithinRoot(resolved, root) {
		// The symlink-resolved target escapes the root being searched, so
		// a symlink can't be used to read files outside the requested
		// directory.
		return walkEntrySkip, ""
	}
	return walkEntryGrep, resolved
}

// GrepContent searches path for lines matching a regular expression pattern.
// If path is a directory, recursive must be true to search its files.
//
// A recursive search walks the whole tree under path, which is unbounded from
// this function's point of view, so ctx is honored between entries and lets a
// caller abort one. Note Go needs no equivalent of the Python runtime's regex
// timeout: this uses RE2, which is linear-time and cannot backtrack
// catastrophically, so it is the walk rather than the match that can run long.
func GrepContent(ctx context.Context, path, pattern string, recursive, ignoreCase bool) (string, error) {
	expr := pattern
	if ignoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat %q: %w", path, err)
	}

	var result strings.Builder
	var skipped int
	grepFile := func(filePath string) error {
		return scanFileLines(filePath, func(lineNum int, line string) bool {
			if re.MatchString(line) {
				fmt.Fprintf(&result, "%s:%d:%s\n", filePath, lineNum, truncateRunes(line, maxLineRunes))
			}
			return true
		})
	}

	if info.IsDir() {
		if !recursive {
			return "", fmt.Errorf("%q is a directory; set recursive=true to search directories", path)
		}
		// Reuse the outer err (rather than := , which would shadow it in this
		// block) so a WalkDir failure below is actually observed by the
		// err != nil check after this if/else.
		var root string
		root, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve %q: %w", path, err)
		}
		// Walk the resolved root, not path: filepath.WalkDir uses Lstat on
		// its root argument, so if path itself were an unresolved directory
		// symlink, WalkDir would see a non-directory at the root and never
		// descend into it at all.
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			// Checked per entry rather than per line: a single file's scan is
			// bounded by its size, but the number of entries is not.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if walkErr != nil {
				if p == root {
					// The search root itself couldn't be read (e.g.
					// permission denied): the search never actually ran, so
					// surface a real error instead of a misleadingly
					// confident "no matches found".
					return walkErr
				}
				// WalkDir surfaces ReadDir/Lstat failures (e.g. a
				// permission-denied subdirectory) through this err rather
				// than via grepFile, but it deserves the same treatment: one
				// unreadable subtree shouldn't discard matches already found
				// in its siblings.
				skipped++
				return nil
			}
			switch action, resolved := classifyWalkEntry(root, p, d); action {
			case walkEntryUnreadable:
				skipped++
			case walkEntryGrep:
				// A read error on one file (permission denied, a line
				// exceeding the scan buffer, etc.) shouldn't abort matches
				// already found elsewhere in the tree.
				if grepErr := grepFile(resolved); grepErr != nil {
					skipped++
				}
			}
			return nil
		})
	} else {
		// scanFileLines rejects non-regular files too, but reusing the stat
		// already taken above lets an explicitly-targeted FIFO report the
		// plain reason rather than one wrapped in "failed to search".
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%q is not a regular file", path)
		}
		err = grepFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("failed to search %q: %w", path, err)
	}

	if result.Len() == 0 {
		if skipped > 0 {
			return fmt.Sprintf("no matches found (%d entries could not be read)", skipped), nil
		}
		return "no matches found", nil
	}

	matches := strings.TrimSuffix(result.String(), "\n")
	if skipped > 0 {
		matches += fmt.Sprintf("\n\n(%d entries could not be read)", skipped)
	}
	return matches, nil
}
