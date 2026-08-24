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
kagent_grpc_url: grpc.kagent.example.test:443
kagent_grpc_tls: true
kagent_grpc_ca_file: /tmp/kagent-ca.pem
kagent_grpc_server_name: grpc.kagent.example.test
namespace: configured-ns
output_format: json
verbose: true
timeout: 45s
user_id: configured-user
`), 0600))

	cfg, err := loadConfig()
	require.NoError(t, err)

	assert.Equal(t, "http://kagent.example.test", cfg.KAgentURL)
	assert.Equal(t, "grpc.kagent.example.test:443", cfg.KAgentGRPCURL)
	assert.True(t, cfg.KAgentGRPCTLS)
	assert.Equal(t, "/tmp/kagent-ca.pem", cfg.KAgentGRPCCAFile)
	assert.Equal(t, "grpc.kagent.example.test", cfg.KAgentGRPCServerName)
	assert.Equal(t, "configured-ns", cfg.Namespace)
	assert.Equal(t, "json", cfg.OutputFormat)
	assert.True(t, cfg.Verbose)
	assert.Equal(t, 45*time.Second, cfg.Timeout)
	assert.Equal(t, "configured-user", cfg.UserID)
}

func TestLoadConfigDoesNotWriteDefaults(t *testing.T) {
	resetConfigState(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, config.DefaultUserID, cfg.UserID)
	_, err = os.Stat(filepath.Join(homeDir, ".kagent"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadConfigReadsUserIDFromEnvironment(t *testing.T) {
	resetConfigState(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("KAGENT_USER_ID", "environment-user")

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "environment-user", cfg.UserID)
}

func TestLoadConfigRejectsInvalidUserID(t *testing.T) {
	resetConfigState(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("KAGENT_USER_ID", "invalid user")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caller identity must not contain whitespace")
}

func TestRootCommandUsesConfigValuesAsFlagDefaults(t *testing.T) {
	cfg := &config.Config{
		KAgentURL:            "http://kagent.example.test",
		KAgentGRPCURL:        "grpc.kagent.example.test:443",
		KAgentGRPCTLS:        true,
		KAgentGRPCCAFile:     "/tmp/kagent-ca.pem",
		KAgentGRPCServerName: "grpc.kagent.example.test",
		Namespace:            "configured-ns",
		OutputFormat:         "json",
		Verbose:              true,
		Timeout:              45 * time.Second,
		UserID:               "configured-user",
	}

	rootCmd := newRootCommand(context.Background(), cfg)

	assert.Equal(t, "http://kagent.example.test", rootCmd.PersistentFlags().Lookup("kagent-url").DefValue)
	assert.Equal(t, "grpc.kagent.example.test:443", rootCmd.PersistentFlags().Lookup("kagent-grpc-url").DefValue)
	assert.Equal(t, "true", rootCmd.PersistentFlags().Lookup("kagent-grpc-tls").DefValue)
	assert.Equal(t, "/tmp/kagent-ca.pem", rootCmd.PersistentFlags().Lookup("kagent-grpc-ca-file").DefValue)
	assert.Equal(t, "grpc.kagent.example.test", rootCmd.PersistentFlags().Lookup("kagent-grpc-server-name").DefValue)
	assert.Equal(t, "configured-ns", rootCmd.PersistentFlags().Lookup("namespace").DefValue)
	assert.Equal(t, "json", rootCmd.PersistentFlags().Lookup("output-format").DefValue)
	assert.Equal(t, "true", rootCmd.PersistentFlags().Lookup("verbose").DefValue)
	assert.Equal(t, "45s", rootCmd.PersistentFlags().Lookup("timeout").DefValue)
	assert.Equal(t, "configured-user", rootCmd.PersistentFlags().Lookup("user-id").DefValue)

	deployCmd, _, err := rootCmd.Find([]string{"deploy"})
	require.NoError(t, err)
	require.NotNil(t, deployCmd)

	assert.Equal(t, "configured-ns", deployCmd.Flags().Lookup("namespace").DefValue)
	assert.Equal(t, "configured-ns", cfg.Namespace)
}

func TestRootCommandFlagsOverrideConfigValues(t *testing.T) {
	cfg := &config.Config{
		KAgentURL:     "http://kagent.example.test",
		KAgentGRPCURL: "grpc.kagent.example.test:443",
		Namespace:     "configured-ns",
		OutputFormat:  "json",
		Verbose:       false,
		Timeout:       45 * time.Second,
	}

	rootCmd := newRootCommand(context.Background(), cfg)
	require.NoError(t, rootCmd.ParseFlags([]string{
		"--kagent-url", "http://flag.example.test",
		"--kagent-grpc-url", "grpc.flag.example.test:8443",
		"--kagent-grpc-tls",
		"--kagent-grpc-ca-file", "/tmp/flag-ca.pem",
		"--kagent-grpc-server-name", "grpc.flag.example.test",
		"--namespace", "flag-ns",
		"--output-format", "yaml",
		"--verbose",
		"--timeout", "10s",
		"--user-id", "flag-user",
	}))

	assert.Equal(t, "http://flag.example.test", cfg.KAgentURL)
	assert.Equal(t, "grpc.flag.example.test:8443", cfg.KAgentGRPCURL)
	assert.True(t, cfg.KAgentGRPCTLS)
	assert.Equal(t, "/tmp/flag-ca.pem", cfg.KAgentGRPCCAFile)
	assert.Equal(t, "grpc.flag.example.test", cfg.KAgentGRPCServerName)
	assert.Equal(t, "flag-ns", cfg.Namespace)
	assert.Equal(t, "yaml", cfg.OutputFormat)
	assert.True(t, cfg.Verbose)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
	assert.Equal(t, "flag-user", cfg.UserID)
}

func TestRootCommandRejectsInvalidUserIDFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "empty", args: []string{"--user-id=", "version"}},
		{name: "whitespace", args: []string{"--user-id", "invalid user", "version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				KAgentURL:     config.DefaultKAgentURL,
				KAgentGRPCURL: config.DefaultKAgentGRPCURL,
				UserID:        config.DefaultUserID,
			}
			rootCmd := newRootCommand(t.Context(), cfg)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.ExecuteContext(t.Context())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "caller identity")
		})
	}
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
