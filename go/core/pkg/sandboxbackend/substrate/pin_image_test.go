package substrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinImageRef(t *testing.T) {
	t.Run("accepts digest pin", func(t *testing.T) {
		ref := "ghcr.io/kagent-dev/nemoclaw/sandbox-base@sha256:abc"
		got, err := pinImageRef(ref)
		require.NoError(t, err)
		require.Equal(t, ref, got)
	})

	t.Run("rejects tag", func(t *testing.T) {
		_, err := pinImageRef("ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4")
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be pinned with a digest")
	})

	t.Run("rejects empty", func(t *testing.T) {
		_, err := pinImageRef("  ")
		require.Error(t, err)
	})
}
