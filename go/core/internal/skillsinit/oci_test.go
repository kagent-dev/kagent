package skillsinit

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_tarEntryToLocal_rejectsEscape covers every shape of tar-entry name
// that the original `tar xf` pipeline would have happily honored: absolute
// paths, ".." traversal, and combinations thereof. A malicious skill image
// is the motivating threat — these names must never produce paths outside dst.
func Test_tarEntryToLocal_rejectsEscape(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		wantErr bool
	}{
		{"plain file", "file.txt", false},
		{"nested file", "a/b/c.txt", false},
		{"dot-only", ".", false},
		{"leading slash stripped", "/file.txt", false}, // re-rooted under dst, not at /
		{"traversal", "../escape", true},
		{"traversal mid-path", "a/../../escape", true},
		{"absolute escape", "/etc/passwd", false}, // strips leading "/" so result is dst/etc/passwd — under dst, intentional
		{"deep traversal", "../../../etc/passwd", true},
		{"trailing traversal", "a/b/../../..", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tarEntryToLocal(tc.entry)
			if tc.wantErr {
				require.Error(t, err, "tarEntryToLocal(%q) must reject", tc.entry)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test_extractTar_rejectsPathTraversalEntry feeds a hand-crafted tar with a
// "../escape" entry. The old shell pipeline would have written outside the
// destination; extractTar must error and not create any file.
func Test_extractTar_rejectsPathTraversalEntry(t *testing.T) {
	dst := t.TempDir()
	buf := tarOf(t, tarEntry{Name: "../escape.txt", Mode: 0o644, Body: []byte("pwned")})
	err := extractTar(buf, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes destination")

	// Sanity: nothing was created either inside dst or as a sibling.
	_, statErr := os.Stat(filepath.Join(filepath.Dir(dst), "escape.txt"))
	require.True(t, os.IsNotExist(statErr), "sibling file must not exist")
}

// Test_extractTar_rejectsAbsoluteSymlink mirrors the OCI test corpus the
// previous container shipped (e.g. distroless's /etc/localtime symlink).
// We refuse rather than risk writing outside the volume.
func Test_extractTar_rejectsAbsoluteSymlink(t *testing.T) {
	dst := t.TempDir()
	buf := tarOf(t, tarEntry{
		Name:     "localtime",
		LinkName: "/etc/passwd",
		Type:     tar.TypeSymlink,
	})
	err := extractTar(buf, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute symlink")
}

// Test_extractTar_rejectsEscapingSymlink covers relative symlinks whose
// resolved target points outside dst.
func Test_extractTar_rejectsEscapingSymlink(t *testing.T) {
	dst := t.TempDir()
	buf := tarOf(t, tarEntry{
		Name:     "link",
		LinkName: "../../etc/passwd",
		Type:     tar.TypeSymlink,
	})
	err := extractTar(buf, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes destination")
}

// Test_extractTar_acceptsBenignSymlink ensures we haven't broken the legitimate
// in-tree symlink case (e.g., a/b -> a/c).
func Test_extractTar_acceptsBenignSymlink(t *testing.T) {
	dst := t.TempDir()
	buf := tarOf(t,
		tarEntry{Name: "target.txt", Mode: 0o644, Body: []byte("hi")},
		tarEntry{Name: "link.txt", LinkName: "target.txt", Type: tar.TypeSymlink},
	)
	require.NoError(t, extractTar(buf, dst))
	got, err := os.Readlink(filepath.Join(dst, "link.txt"))
	require.NoError(t, err)
	assert.Equal(t, "target.txt", got)
}

// Test_extractTar_writesRegularFiles is the smoke test that confirms the
// rewritten extractor still writes normal entries — without this, the negative
// tests above could pass by being unconditionally restrictive.
func Test_extractTar_writesRegularFiles(t *testing.T) {
	dst := t.TempDir()
	buf := tarOf(t,
		tarEntry{Name: "sub/", Mode: 0o755, Type: tar.TypeDir},
		tarEntry{Name: "sub/a.txt", Mode: 0o644, Body: []byte("hello")},
	)
	require.NoError(t, extractTar(buf, dst))
	body, err := os.ReadFile(filepath.Join(dst, "sub", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}

// tarEntry is a minimal description of one tar record.
type tarEntry struct {
	Name     string
	Mode     int64
	Body     []byte
	LinkName string
	Type     byte
}

// tarOf assembles a tar stream in memory for use as input to extractTar.
func tarOf(t *testing.T, entries ...tarEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		typ := e.Type
		if typ == 0 {
			if e.LinkName != "" {
				typ = tar.TypeSymlink
			} else if strings.HasSuffix(e.Name, "/") {
				typ = tar.TypeDir
			} else {
				typ = tar.TypeReg
			}
		}
		hdr := &tar.Header{
			Name:     e.Name,
			Mode:     e.Mode,
			Size:     int64(len(e.Body)),
			Typeflag: typ,
			Linkname: e.LinkName,
		}
		if typ != tar.TypeReg {
			hdr.Size = 0
		}
		require.NoError(t, w.WriteHeader(hdr))
		if typ == tar.TypeReg && len(e.Body) > 0 {
			_, err := w.Write(e.Body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, w.Close())
	return &buf
}
