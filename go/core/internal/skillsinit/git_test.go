package skillsinit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit("https://example.com/repository.git", "main", t.TempDir())
	require.ErrorContains(t, err, "full SHA")
}
