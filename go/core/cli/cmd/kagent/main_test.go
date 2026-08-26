package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandUsesOptionValuesAsFlagDefaults(t *testing.T) {
	opts := &rootOptions{
		Connection: connection.Options{
			KAgentURL:            "http://kagent.example.test",
			KAgentGRPCURL:        "grpc.kagent.example.test:443",
			KAgentGRPCTLS:        true,
			KAgentGRPCCAFile:     "/tmp/kagent-ca.pem",
			KAgentGRPCServerName: "grpc.kagent.example.test",
			Namespace:            "configured-ns",
			Verbose:              true,
			Timeout:              45 * time.Second,
			UserID:               "configured-user",
		},
		OutputFormat: "json",
	}

	rootCmd := newRootCommand(context.Background(), opts)

	assert.Equal(t, "http://kagent.example.test", rootCmd.PersistentFlags().Lookup("kagent-url").DefValue)
	assert.Equal(t, "grpc.kagent.example.test:443", rootCmd.PersistentFlags().Lookup("grpc-url").DefValue)
	assert.Equal(t, "true", rootCmd.PersistentFlags().Lookup("grpc-tls").DefValue)
	assert.Equal(t, "/tmp/kagent-ca.pem", rootCmd.PersistentFlags().Lookup("grpc-ca-file").DefValue)
	assert.Equal(t, "grpc.kagent.example.test", rootCmd.PersistentFlags().Lookup("grpc-server-name").DefValue)
	assert.Equal(t, "configured-ns", rootCmd.PersistentFlags().Lookup("namespace").DefValue)
	assert.Equal(t, "json", rootCmd.PersistentFlags().Lookup("output-format").DefValue)
	assert.Equal(t, "true", rootCmd.PersistentFlags().Lookup("verbose").DefValue)
	assert.Equal(t, "45s", rootCmd.PersistentFlags().Lookup("timeout").DefValue)
	assert.Equal(t, "configured-user", rootCmd.PersistentFlags().Lookup("user-id").DefValue)

	assert.Equal(t, "configured-ns", opts.Connection.Namespace)
}

func TestRootCommandFlagsOverrideOptionValues(t *testing.T) {
	opts := &rootOptions{
		Connection: connection.Options{
			KAgentURL:     "http://kagent.example.test",
			KAgentGRPCURL: "grpc.kagent.example.test:443",
			Namespace:     "configured-ns",
			Timeout:       45 * time.Second,
		},
		OutputFormat: "json",
	}

	rootCmd := newRootCommand(context.Background(), opts)
	require.NoError(t, rootCmd.ParseFlags([]string{
		"--kagent-url", "http://flag.example.test",
		"--grpc-url", "grpc.flag.example.test:8443",
		"--grpc-tls",
		"--grpc-ca-file", "/tmp/flag-ca.pem",
		"--grpc-server-name", "grpc.flag.example.test",
		"--namespace", "flag-ns",
		"--output-format", "yaml",
		"--verbose",
		"--timeout", "10s",
		"--user-id", "flag-user",
	}))

	assert.Equal(t, "http://flag.example.test", opts.Connection.KAgentURL)
	assert.Equal(t, "grpc.flag.example.test:8443", opts.Connection.KAgentGRPCURL)
	assert.True(t, opts.Connection.KAgentGRPCTLS)
	assert.Equal(t, "/tmp/flag-ca.pem", opts.Connection.KAgentGRPCCAFile)
	assert.Equal(t, "grpc.flag.example.test", opts.Connection.KAgentGRPCServerName)
	assert.Equal(t, "flag-ns", opts.Connection.Namespace)
	assert.Equal(t, "yaml", opts.OutputFormat)
	assert.True(t, opts.Connection.Verbose)
	assert.Equal(t, 10*time.Second, opts.Connection.Timeout)
	assert.Equal(t, "flag-user", opts.Connection.UserID)
}

func TestRootCommandDoesNotValidateClientFlagsForIndependentCommand(t *testing.T) {
	rootCmd := newRootCommand(t.Context(), defaultRootOptions())
	rootCmd.SetArgs([]string{"--output-format", "yaml", "--user-id", "invalid user", "env"})
	rootCmd.SetOut(&bytes.Buffer{})

	require.NoError(t, rootCmd.ExecuteContext(t.Context()))
}

func TestRootCommandInvokeContract(t *testing.T) {
	rootCmd := newRootCommand(t.Context(), defaultRootOptions())
	assert.True(t, rootCmd.SilenceErrors)
	assert.True(t, rootCmd.SilenceUsage)

	invokeCmd, _, err := rootCmd.Find([]string{"invoke"})
	require.NoError(t, err)
	for _, flag := range []string{"agent-instance", "task", "file", "stream", "token"} {
		assert.NotNil(t, invokeCmd.Flags().Lookup(flag), "missing --%s", flag)
	}
	for _, legacyFlag := range []string{"agent", "session", "url-override"} {
		assert.Nil(t, invokeCmd.Flags().Lookup(legacyFlag), "legacy --%s must be removed", legacyFlag)
	}

	getInstanceCmd, _, err := rootCmd.Find([]string{"get", "agent-instance"})
	require.NoError(t, err)
	assert.Equal(t, "agent-instance [ID]", getInstanceCmd.Use)
	for _, flag := range []string{"page-size", "page-token"} {
		assert.NotNil(t, getInstanceCmd.Flags().Lookup(flag), "missing --%s", flag)
	}
}

func TestRootCommandV2CatalogAndLifecycleContract(t *testing.T) {
	rootCmd := newRootCommand(t.Context(), defaultRootOptions())

	getTemplateCmd, _, err := rootCmd.Find([]string{"get", "agent-template"})
	require.NoError(t, err)
	assert.Equal(t, "agent-template [NAME]", getTemplateCmd.Use)
	for _, flag := range []string{"page-size", "page-token"} {
		assert.NotNil(t, getTemplateCmd.Flags().Lookup(flag), "missing --%s", flag)
	}

	createInstanceCmd, _, err := rootCmd.Find([]string{"create", "agent-instance"})
	require.NoError(t, err)
	assert.Equal(t, "agent-instance", createInstanceCmd.Use)
	for _, flag := range []string{"harness", "agent-template", "request-id"} {
		assert.NotNil(t, createInstanceCmd.Flags().Lookup(flag), "missing --%s", flag)
	}

	deleteInstanceCmd, _, err := rootCmd.Find([]string{"delete", "agent-instance"})
	require.NoError(t, err)
	assert.Equal(t, "agent-instance ID", deleteInstanceCmd.Use)

	for _, command := range []string{"suspend", "resume"} {
		_, _, err := rootCmd.Find([]string{command, "agent-instance"})
		assert.Error(t, err, "%s must not be exposed by the CLI", command)
	}
}

func TestRootCommandRemovesLegacyPaths(t *testing.T) {
	rootCmd := newRootCommand(t.Context(), defaultRootOptions())

	rootCommands := make([]string, 0, len(rootCmd.Commands()))
	for _, command := range rootCmd.Commands() {
		rootCommands = append(rootCommands, command.Name())
	}
	for _, command := range []string{"deploy", "init", "build", "run", "add-mcp"} {
		assert.NotContains(t, rootCommands, command)
	}
	assert.Contains(t, rootCommands, "mcp")

	getCmd, _, err := rootCmd.Find([]string{"get"})
	require.NoError(t, err)
	getCommands := make([]string, 0, len(getCmd.Commands()))
	for _, command := range getCmd.Commands() {
		getCommands = append(getCommands, command.Name())
	}
	for _, command := range []string{"agent", "session", "tool"} {
		assert.NotContains(t, getCommands, command)
	}
}

func TestRootCommandRequiresTerminalForInteractiveUse(t *testing.T) {
	rootCmd := newRootCommand(t.Context(), defaultRootOptions())
	rootCmd.SetArgs(nil)
	rootCmd.SetIn(&bytes.Buffer{})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.ExecuteContext(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kagent requires a terminal")
	assert.Contains(t, err.Error(), "kagent invoke")
}
