package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteCRDsLeavesLegacyGroupAlone(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "kubectl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KAGENT_TEST_KUBECTL_LOG\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KAGENT_TEST_KUBECTL_LOG", logPath)

	require.NoError(t, deleteCRDs(t.Context()))
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"delete crd agents.api.kagent.dev",
		"delete crd agenttemplates.api.kagent.dev",
		"delete crd harnesses.api.kagent.dev",
		"delete crd modelconfigs.api.kagent.dev",
		"delete crd modelproviderconfigs.api.kagent.dev",
		"delete crd remotemcpservers.api.kagent.dev",
	}, strings.Split(strings.TrimSpace(string(data)), "\n"))
}
