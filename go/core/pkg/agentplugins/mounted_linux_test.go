package agentplugins

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestValidateMountedPackageRejectsNamedPipe(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, unix.Mkfifo(filepath.Join(root, "pipe"), 0o600))
	// A mounted image can expose special files that the old extractor skipped.
	// Refuse them before copying, which would block on a FIFO.
	require.ErrorContains(t, validatePackage(root), "unsupported file")
}
