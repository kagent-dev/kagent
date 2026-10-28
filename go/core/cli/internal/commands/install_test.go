package commands

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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
