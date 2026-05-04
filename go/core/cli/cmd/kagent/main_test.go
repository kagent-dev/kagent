package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigReadsConfigFileValues(t *testing.T) {
	resetConfigState(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configDir := filepath.Join(homeDir, ".kagent")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(`
kagent_url: http://kagent.example.test
namespace: configured-ns
output_format: json
verbose: true
timeout: 45s
`), 0600))

	cfg, err := loadConfig()
	require.NoError(t, err)

	assert.Equal(t, "http://kagent.example.test", cfg.KAgentURL)
	assert.Equal(t, "configured-ns", cfg.Namespace)
	assert.Equal(t, "json", cfg.OutputFormat)
	assert.True(t, cfg.Verbose)
	assert.Equal(t, 45*time.Second, cfg.Timeout)
}

func TestRootCommandUsesConfigValuesAsFlagDefaults(t *testing.T) {
	cfg := &config.Config{
		KAgentURL:    "http://kagent.example.test",
		Namespace:    "configured-ns",
		OutputFormat: "json",
		Verbose:      true,
		Timeout:      45 * time.Second,
	}

	rootCmd := newRootCommand(context.Background(), cfg)

	assert.Equal(t, "http://kagent.example.test", rootCmd.PersistentFlags().Lookup("kagent-url").DefValue)
	assert.Equal(t, "configured-ns", rootCmd.PersistentFlags().Lookup("namespace").DefValue)
	assert.Equal(t, "json", rootCmd.PersistentFlags().Lookup("output-format").DefValue)
	assert.Equal(t, "true", rootCmd.PersistentFlags().Lookup("verbose").DefValue)
	assert.Equal(t, "45s", rootCmd.PersistentFlags().Lookup("timeout").DefValue)

	deployCmd, _, err := rootCmd.Find([]string{"deploy"})
	require.NoError(t, err)
	require.NotNil(t, deployCmd)

	assert.Equal(t, "configured-ns", deployCmd.Flags().Lookup("namespace").DefValue)
	assert.Equal(t, "configured-ns", cfg.Namespace)
}

func TestRootCommandFlagsOverrideConfigValues(t *testing.T) {
	cfg := &config.Config{
		KAgentURL:    "http://kagent.example.test",
		Namespace:    "configured-ns",
		OutputFormat: "json",
		Verbose:      false,
		Timeout:      45 * time.Second,
	}

	rootCmd := newRootCommand(context.Background(), cfg)
	require.NoError(t, rootCmd.ParseFlags([]string{
		"--kagent-url", "http://flag.example.test",
		"--namespace", "flag-ns",
		"--output-format", "yaml",
		"--verbose",
		"--timeout", "10s",
	}))

	assert.Equal(t, "http://flag.example.test", cfg.KAgentURL)
	assert.Equal(t, "flag-ns", cfg.Namespace)
	assert.Equal(t, "yaml", cfg.OutputFormat)
	assert.True(t, cfg.Verbose)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
}

func resetConfigState(t *testing.T) {
	t.Helper()

	oldCommandLine := pflag.CommandLine
	viper.Reset()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)

	t.Cleanup(func() {
		viper.Reset()
		pflag.CommandLine = oldCommandLine
	})
}
