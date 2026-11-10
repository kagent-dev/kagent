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

// withinRoot checks containment after callers have resolved symlinks.
// This bounds search scope; Substrate owns Actor isolation.
func withinRoot(resolved, root string) bool {
	relative, err := filepath.Rel(root, resolved)
	return err == nil && filepath.IsLocal(relative)
}

type walkEntryAction int

const (
	walkEntryGrep walkEntryAction = iota
	walkEntrySkip
	walkEntryUnreadable
)

// classifyWalkEntry skips special files and symlinks outside the requested
// search tree. It does not impose an additional sandbox boundary.
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
	if !withinRoot(resolved, root) {
		// The symlink-resolved target escapes the root being searched, so
		// a symlink can't be used to read files outside the requested
		// directory.
		return walkEntrySkip, ""
	}
	return walkEntryGrep, resolved
}

// grepContent searches path for lines matching a regular expression pattern.
// If path is a directory, recursive must be true to search its files.
//
// Search results and per-line buffers are bounded; cancellation is checked per line.
func grepContent(ctx context.Context, path, pattern string, recursive, ignoreCase bool) (string, error) {
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
	truncated := false
	grepFile := func(filePath string) error {
		return scanFileLines(ctx, filePath, func(lineNum int, line string) bool {
			if re.MatchString(line) {
				if !appendResultLine(&result, fmt.Sprintf("%s:%d:%s", filePath, lineNum, truncateRunes(line, maxLineRunes))) {
					truncated = true
					return false
				}
			}
			return true
		})
	}

	if info.IsDir() {
		if !recursive {
			return "", fmt.Errorf("%q is a directory; set recursive=true to search directories", path)
		}
		var root string
		root, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve %q: %w", path, err)
		}
		// WalkDir does not descend through an unresolved symlink root.
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
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
					if ctx.Err() != nil {
						return ctx.Err()
					}
					skipped++
				}
				if truncated {
					return fs.SkipAll
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
