package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestClassifyWalkEntry pins the three-way split directly rather than only
// through TestGrepContent's integration assertions, so a change to the
// classifier fails here with the specific case that broke.
//
// It is the deliberate mirror of Python's
// test_classify_walk_entry_covers_each_outcome (kagent-skills, shell.py):
// same cases, same order, same expected outcomes. The two runtimes must sort
// a tree identically, and reading these two tests side by side is how that
// stays true. Prefer changing both, or neither.
//
// The directory case has no Python counterpart on purpose -- filepath.WalkDir
// hands this function directories, while Python's os.walk yields only
// filenames, so only Go can reach it.
func TestClassifyWalkEntry(t *testing.T) {
	root := createTempDir(t)
	defer os.RemoveAll(root)

	plain := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(plain, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	// A sibling temp dir, not filepath.Dir(root): that would be the shared
	// system temp directory, where a fixed filename races other runs of this
	// package.
	outsideDir := createTempDir(t)
	defer os.RemoveAll(outsideDir)
	outside := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0644); err != nil {
		t.Fatalf("failed to write out-of-root file: %v", err)
	}

	for name, target := range map[string]string{
		"inside-link":   "plain.txt",
		"escaping-link": outside,
		"broken-link":   filepath.Join(root, "does-not-exist"),
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
	}
	loop := filepath.Join(root, "loop-link")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	fifoErr := syscall.Mkfifo(filepath.Join(root, "pipe"), 0644)

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root) error = %v", err)
	}
	resolvedPlain, err := filepath.EvalSymlinks(plain)
	if err != nil {
		t.Fatalf("EvalSymlinks(plain) error = %v", err)
	}

	entries := map[string]fs.DirEntry{}
	dirEntries, err := os.ReadDir(resolvedRoot)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, d := range dirEntries {
		entries[d.Name()] = d
	}

	tests := []struct {
		name         string
		entry        string
		wantAction   walkEntryAction
		wantResolved string
		why          string
	}{
		{"regular file", "plain.txt", walkEntryGrep, resolvedPlain,
			"a normal in-bounds file is the whole point"},
		{"symlink inside the root", "inside-link", walkEntryGrep, resolvedPlain,
			"resolves through to its target so the caller reads what was just checked"},
		{"symlink escaping the root", "escaping-link", walkEntrySkip, "",
			"a symlink must not be usable to read outside the searched directory"},
		{"broken symlink", "broken-link", walkEntryUnreadable, "",
			"a dangling link is a real read failure, not a policy exclusion"},
		{"symlink loop", "loop-link", walkEntryUnreadable, "",
			"same class of failure as a dangling link"},
		{"directory", "subdir", walkEntrySkip, "",
			"WalkDir descends on its own; Python never reaches this case"},
		{"fifo", "pipe", walkEntrySkip, "",
			"excluded by policy, not failure -- opening one can block forever"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.entry == "pipe" && fifoErr != nil {
				t.Skipf("FIFOs not supported: %v", fifoErr)
			}
			d, ok := entries[tt.entry]
			if !ok {
				t.Fatalf("test setup: no dir entry named %q", tt.entry)
			}
			action, resolved := classifyWalkEntry(resolvedRoot, filepath.Join(resolvedRoot, tt.entry), d)
			if action != tt.wantAction {
				t.Errorf("classifyWalkEntry() action = %v, want %v (%s)", action, tt.wantAction, tt.why)
			}
			if resolved != tt.wantResolved {
				t.Errorf("classifyWalkEntry() resolved = %q, want %q", resolved, tt.wantResolved)
			}
		})
	}
}

func TestGrepContent(t *testing.T) {
	tmpDir := createTempDir(t)
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nFOO bar\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("another foo line\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	t.Run("matches within a single file", func(t *testing.T) {
		result, err := GrepContent(context.Background(), filepath.Join(tmpDir, "a.txt"), "hello", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "a.txt:1:hello world") {
			t.Errorf("expected match with path:line:content, got %q", result)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		result, err := GrepContent(context.Background(), filepath.Join(tmpDir, "a.txt"), "nope", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if result != "no matches found" {
			t.Errorf("expected no matches message, got %q", result)
		}
	})

	t.Run("ignore case", func(t *testing.T) {
		result, err := GrepContent(context.Background(), filepath.Join(tmpDir, "a.txt"), "foo", false, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "FOO bar") {
			t.Errorf("expected case-insensitive match, got %q", result)
		}
	})

	t.Run("directory requires recursive", func(t *testing.T) {
		if _, err := GrepContent(context.Background(), tmpDir, "foo", false, false); err == nil {
			t.Fatal("expected error when searching a directory without recursive=true")
		}
	})

	t.Run("recursive searches subdirectories", func(t *testing.T) {
		result, err := GrepContent(context.Background(), tmpDir, "foo", true, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "b.txt:1:another foo line") {
			t.Errorf("expected match from subdirectory, got %q", result)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		if _, err := GrepContent(context.Background(), filepath.Join(tmpDir, "a.txt"), "(", false, false); err == nil {
			t.Fatal("expected error for invalid regex pattern")
		}
	})

	t.Run("recursive search skips symlinks that escape the root", func(t *testing.T) {
		outsideDir := createTempDir(t)
		defer os.RemoveAll(outsideDir)
		secretPath := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(secretPath, []byte("top secret foo\n"), 0644); err != nil {
			t.Fatalf("Failed to write outside file: %v", err)
		}

		linkPath := filepath.Join(subDir, "escape.txt")
		if err := os.Symlink(secretPath, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		result, err := GrepContent(context.Background(), tmpDir, "foo", true, true)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if strings.Contains(result, "top secret") {
			t.Errorf("expected symlinked file outside root to be skipped, got %q", result)
		}
	})

	t.Run("recursive search greps the resolved target of an in-bounds file symlink", func(t *testing.T) {
		realFile := filepath.Join(subDir, "real_target.txt")
		if err := os.WriteFile(realFile, []byte("foo via symlink\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		linkPath := filepath.Join(subDir, "file_link.txt")
		if err := os.Symlink(realFile, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		result, err := GrepContent(context.Background(), subDir, "foo via symlink", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		// The match must be reported against the resolved target path
		// (real_target.txt), not the walked symlink path (file_link.txt):
		// classifyWalkEntry verifies the resolved target is in-bounds, and
		// the actual read must use that same resolved value rather than
		// re-deriving/reopening the raw symlink path, or the verified-safe
		// check and the actual read could diverge (TOCTOU).
		if !strings.Contains(result, "real_target.txt:1:foo via symlink") {
			t.Errorf("expected match to be reported against the resolved target path, got %q", result)
		}
		if strings.Contains(result, "file_link.txt:") {
			t.Errorf("expected match not to be reported against the unresolved symlink path, got %q", result)
		}
	})

	t.Run("recursive search does not abort on an in-bounds directory symlink", func(t *testing.T) {
		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		if err := os.WriteFile(filepath.Join(walkDir, "aaa_first.txt"), []byte("foo one\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		realSub := filepath.Join(walkDir, "real_sub")
		if err := os.Mkdir(realSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}

		// A symlink to an in-bounds directory, lexically sorted between the
		// two files below, so an incorrect abort partway through the walk
		// would silently drop "zzz_sub"'s match.
		linkPath := filepath.Join(walkDir, "mmm_link")
		if err := os.Symlink(realSub, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		defer os.Remove(linkPath)

		zzzSub := filepath.Join(walkDir, "zzz_sub")
		if err := os.Mkdir(zzzSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(zzzSub, "zzz_last.txt"), []byte("foo two\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		result, err := GrepContent(context.Background(), walkDir, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "aaa_first.txt:1:foo one") {
			t.Errorf("expected match before the symlink, got %q", result)
		}
		if !strings.Contains(result, "zzz_last.txt:1:foo two") {
			t.Errorf("expected match after the symlink (walk must not abort on it), got %q", result)
		}
	})

	t.Run("recursive search resolves the root itself when it is an unresolved symlink", func(t *testing.T) {
		realDir := createTempDir(t)
		defer os.RemoveAll(realDir)
		if err := os.WriteFile(filepath.Join(realDir, "match.txt"), []byte("foo inside\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		parentDir := createTempDir(t)
		defer os.RemoveAll(parentDir)
		linkRoot := filepath.Join(parentDir, "link-root")
		if err := os.Symlink(realDir, linkRoot); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		// Pass the unresolved symlink directly, as a caller that doesn't
		// pre-resolve its path would.
		result, err := GrepContent(context.Background(), linkRoot, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "match.txt:1:foo inside") {
			t.Errorf("expected GrepContent to resolve a symlinked root and recurse into it, got %q", result)
		}
	})

	t.Run("recursive search does not hang on a FIFO and finds matches around it", func(t *testing.T) {
		fifoDir := createTempDir(t)
		defer os.RemoveAll(fifoDir)

		if err := os.WriteFile(filepath.Join(fifoDir, "aaa_before.txt"), []byte("foo before\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		fifoPath := filepath.Join(fifoDir, "mmm_pipe")
		if err := syscall.Mkfifo(fifoPath, 0644); err != nil {
			t.Skipf("FIFOs not supported: %v", err)
		}
		if err := os.WriteFile(filepath.Join(fifoDir, "zzz_after.txt"), []byte("foo after\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		done := make(chan struct{})
		var result string
		var err error
		go func() {
			result, err = GrepContent(context.Background(), fifoDir, "foo", true, false)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("GrepContent hung on a FIFO instead of skipping it")
		}

		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "aaa_before.txt:1:foo before") {
			t.Errorf("expected match before the FIFO, got %q", result)
		}
		if !strings.Contains(result, "zzz_after.txt:1:foo after") {
			t.Errorf("expected match after the FIFO (walk must not hang or abort on it), got %q", result)
		}
	})

	t.Run("a single target FIFO returns an error instead of hanging", func(t *testing.T) {
		fifoDir := createTempDir(t)
		defer os.RemoveAll(fifoDir)
		fifoPath := filepath.Join(fifoDir, "pipe")
		if err := syscall.Mkfifo(fifoPath, 0644); err != nil {
			t.Skipf("FIFOs not supported: %v", err)
		}

		done := make(chan struct{})
		var err error
		go func() {
			_, err = GrepContent(context.Background(), fifoPath, "foo", false, false)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("GrepContent hung opening a FIFO directly instead of erroring")
		}

		if err == nil {
			t.Fatal("expected an error for a non-regular file target, got nil")
		}
	})

	t.Run("recursive search does not abort or discard matches on an unreadable subdirectory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions; cannot exercise this case")
		}

		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		okSub := filepath.Join(walkDir, "aaa_ok")
		if err := os.Mkdir(okSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(okSub, "match.txt"), []byte("foo readable\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		noPermSub := filepath.Join(walkDir, "mmm_noperm")
		if err := os.Mkdir(noPermSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(noPermSub, "hidden.txt"), []byte("foo hidden\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		if err := os.Chmod(noPermSub, 0000); err != nil {
			t.Fatalf("Failed to chmod subdir: %v", err)
		}
		defer os.Chmod(noPermSub, 0755)

		result, err := GrepContent(context.Background(), walkDir, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v, expected the unreadable subdirectory to be skipped rather than aborting the whole search", err)
		}
		if !strings.Contains(result, "match.txt:1:foo readable") {
			t.Errorf("expected match from the readable sibling directory to survive an unreadable subdirectory elsewhere in the tree, got %q", result)
		}
	})

	t.Run("no matches found is annotated when entries could not be read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions; cannot exercise this case")
		}

		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		noPermSub := filepath.Join(walkDir, "noperm")
		if err := os.Mkdir(noPermSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(noPermSub, "hidden.txt"), []byte("foo hidden\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		if err := os.Chmod(noPermSub, 0000); err != nil {
			t.Fatalf("Failed to chmod subdir: %v", err)
		}
		defer os.Chmod(noPermSub, 0755)

		result, err := GrepContent(context.Background(), walkDir, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "no matches found") || !strings.Contains(result, "could not be read") {
			t.Errorf("expected an annotated no-matches message noting unreadable entries, got %q", result)
		}
	})

	t.Run("matches are annotated with the skip count when some entries could not be read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions; cannot exercise this case")
		}

		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		okSub := filepath.Join(walkDir, "aaa_ok")
		if err := os.Mkdir(okSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(okSub, "match.txt"), []byte("foo readable\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		noPermSub := filepath.Join(walkDir, "mmm_noperm")
		if err := os.Mkdir(noPermSub, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(noPermSub, "hidden.txt"), []byte("foo hidden\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		if err := os.Chmod(noPermSub, 0000); err != nil {
			t.Fatalf("Failed to chmod subdir: %v", err)
		}
		defer os.Chmod(noPermSub, 0755)

		result, err := GrepContent(context.Background(), walkDir, "foo", true, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "match.txt:1:foo readable") {
			t.Errorf("expected the real match to still be reported, got %q", result)
		}
		if !strings.Contains(result, "could not be read") {
			t.Errorf("expected the skip count to be reported alongside real matches, not silently dropped, got %q", result)
		}
	})

	t.Run("recursive search on a fully unreadable root returns an error, not a confident empty result", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions; cannot exercise this case")
		}

		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)
		if err := os.WriteFile(filepath.Join(walkDir, "hidden.txt"), []byte("foo hidden\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		if err := os.Chmod(walkDir, 0000); err != nil {
			t.Fatalf("Failed to chmod root: %v", err)
		}
		defer os.Chmod(walkDir, 0755)

		result, err := GrepContent(context.Background(), walkDir, "foo", true, false)
		if err == nil {
			t.Fatalf("expected an error when the search root itself is unreadable, got a result instead: %q", result)
		}
	})

	t.Run("matched lines are truncated to 2000 characters", func(t *testing.T) {
		tmpDir := createTempDir(t)
		defer os.RemoveAll(tmpDir)

		longLine := "foo " + strings.Repeat("x", 3000)
		if err := os.WriteFile(filepath.Join(tmpDir, "long.txt"), []byte(longLine+"\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		result, err := GrepContent(context.Background(), filepath.Join(tmpDir, "long.txt"), "foo", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.HasSuffix(result, "...") {
			t.Errorf("expected truncated line to end with '...', got %q", result)
		}
		if len(result) > 2100 {
			t.Errorf("expected result to be truncated to roughly 2000 chars, got length %d", len(result))
		}
	})

	t.Run("recursive search honors a cancelled context", func(t *testing.T) {
		walkDir := createTempDir(t)
		defer os.RemoveAll(walkDir)

		// Enough entries that the walk is guaranteed to consult ctx at least
		// once after cancellation rather than finishing outright.
		for i := range 50 {
			name := filepath.Join(walkDir, fmt.Sprintf("file_%02d.txt", i))
			if err := os.WriteFile(name, []byte("foo\n"), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already cancelled before the walk starts

		_, err := GrepContent(ctx, walkDir, "foo", true, false)
		if err == nil {
			t.Fatal("expected a cancelled context to abort the search, got no error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected the error to wrap context.Canceled, got %v", err)
		}
	})

	t.Run("a non-recursive search is unaffected by context", func(t *testing.T) {
		// Single-file reads are bounded by file size, so they intentionally
		// don't consult ctx -- pinning that so the cancellation check isn't
		// later "helpfully" moved somewhere it would break normal reads.
		tmpDir := createTempDir(t)
		defer os.RemoveAll(tmpDir)
		if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("foo\n"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := GrepContent(ctx, filepath.Join(tmpDir, "a.txt"), "foo", false, false)
		if err != nil {
			t.Fatalf("GrepContent() error = %v", err)
		}
		if !strings.Contains(result, "a.txt:1:foo") {
			t.Errorf("expected the single-file match, got %q", result)
		}
	})
}
