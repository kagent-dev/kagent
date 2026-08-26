package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The controller serves no Agent, Tool, or Model service, and its Session identity model is wrong.
func TestTUIReachesOnlyV2ControlPlane(t *testing.T) {
	forbidden := []string{
		"api/httpapi",
		"api/database",
		"client.Agent.",
		"client.Session.",
		"client.Tool.",
		"client.Model.",
	}

	for _, dir := range []string{".", "instance", "theme"} {
		sources, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, sources, "no Go sources in %s", dir)

		for _, source := range sources {
			if strings.HasSuffix(source, "_test.go") {
				continue
			}
			content, err := os.ReadFile(source)
			require.NoError(t, err)
			for _, legacy := range forbidden {
				assert.NotContains(t, string(content), legacy, "%s must not reach %s", source, legacy)
			}
		}
	}
}
