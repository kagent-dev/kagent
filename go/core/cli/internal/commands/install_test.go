package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

func TestInstallFlagsAndChartConfig(t *testing.T) {
	cmd := NewInstallCmd()
	require.NotNil(t, cmd.Flags().Lookup("skip-database-setup"))
	require.NotNil(t, cmd.Flags().Lookup("profile"))
	settings := &configv1.ChartConfig{Repository: new("./charts/"), Version: new("v1.2.3"), ValuesFiles: []string{"custom.yaml"}}
	chart := chartConfig(settings, "kagent")
	require.Equal(t, "kagent", chart.Name)
	require.Equal(t, "./charts/", chart.Repository)
	require.Equal(t, "v1.2.3", chart.Version)
	require.Equal(t, []string{"custom.yaml"}, chart.ValuesFiles)
	chart.ValuesFiles[0] = "changed.yaml"
	require.Equal(t, "custom.yaml", settings.ValuesFiles[0])
}

func TestProviderAPIKeySelection(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "google")
	t.Setenv("GEMINI_API_KEY", "gemini")
	key, ok := providerAPIKey(v1alpha3.ModelProviderGemini)
	require.True(t, ok)
	require.Equal(t, "google", key.Get())
	t.Setenv("GOOGLE_API_KEY", "")
	key, ok = providerAPIKey(v1alpha3.ModelProviderGemini)
	require.True(t, ok)
	require.Empty(t, key.Get(), "an explicitly empty Google key must not fall back")
	_, ok = providerAPIKey(v1alpha3.ModelProviderBedrock)
	require.False(t, ok)
}

func TestInstallProfileCleansTemporaryValuesOnFailure(t *testing.T) {
	temporary, tools, configDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("install:\n  defaultModelProvider: OpenAI\n"), 0o600))
	t.Setenv("TMPDIR", temporary)
	t.Setenv("PATH", tools)
	t.Setenv("OPENAI_API_KEY", "test-key")
	previousVersion := version.Version
	version.Version = "v1.0.0"
	t.Cleanup(func() { version.Version = previousVersion })
	root := &cobra.Command{Use: "kagent"}
	cliconfig.Register(root)
	connection.RegisterFlags(root.PersistentFlags())
	root.AddCommand(NewInstallCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "install", "--profile", "minimal"})
	require.ErrorContains(t, root.ExecuteContext(t.Context()), "helm not found")
	entries, err := os.ReadDir(temporary)
	require.NoError(t, err)
	require.Empty(t, entries, "the temporary profile must not outlive the install attempt")
}
