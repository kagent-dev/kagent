package cli_test

import (
	"bytes"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandUsesDefaultFlagValues(t *testing.T) {
	rootCmd := cli.Root()

	assert.Equal(t, "http://localhost:8083", rootCmd.PersistentFlags().Lookup("kagent-url").DefValue)
	assert.Equal(t, "localhost:8084", rootCmd.PersistentFlags().Lookup("kagent-grpc-url").DefValue)
	assert.Equal(t, "false", rootCmd.PersistentFlags().Lookup("kagent-grpc-tls").DefValue)
	assert.Empty(t, rootCmd.PersistentFlags().Lookup("kagent-grpc-ca-file").DefValue)
	assert.Empty(t, rootCmd.PersistentFlags().Lookup("kagent-grpc-server-name").DefValue)
	assert.Equal(t, "kagent", rootCmd.PersistentFlags().Lookup("namespace").DefValue)
	assert.Equal(t, "table", rootCmd.PersistentFlags().Lookup("output-format").DefValue)
	assert.Equal(t, "false", rootCmd.PersistentFlags().Lookup("verbose").DefValue)
	assert.Equal(t, "5m0s", rootCmd.PersistentFlags().Lookup("timeout").DefValue)
	assert.Equal(t, "admin@kagent.dev", rootCmd.PersistentFlags().Lookup("user-id").DefValue)
}

func TestRootCommandFlagsOverrideOptionValues(t *testing.T) {
	rootCmd := cli.Root()
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

	want := map[string]string{
		"kagent-url":              "http://flag.example.test",
		"kagent-grpc-url":         "grpc.flag.example.test:8443",
		"kagent-grpc-tls":         "true",
		"kagent-grpc-ca-file":     "/tmp/flag-ca.pem",
		"kagent-grpc-server-name": "grpc.flag.example.test",
		"namespace":               "flag-ns",
		"output-format":           "yaml",
		"verbose":                 "true",
		"timeout":                 "10s",
		"user-id":                 "flag-user",
	}
	for name, value := range want {
		assert.Equal(t, value, rootCmd.PersistentFlags().Lookup(name).Value.String())
	}
}

func TestRootCommandAllowsNoTimeout(t *testing.T) {
	rootCmd := cli.Root()

	require.NoError(t, rootCmd.ParseFlags([]string{"--timeout", "0"}))
	assert.Equal(t, "0s", rootCmd.PersistentFlags().Lookup("timeout").Value.String())
}

func TestRootCommandsOwnIndependentFlagState(t *testing.T) {
	first := cli.Root()
	second := cli.Root()

	require.NoError(t, first.ParseFlags([]string{"--namespace", "first"}))

	assert.Equal(t, "first", first.PersistentFlags().Lookup("namespace").Value.String())
	assert.Equal(t, "kagent", second.PersistentFlags().Lookup("namespace").Value.String())
}

func TestRootCommandDoesNotValidateClientFlagsForIndependentCommand(t *testing.T) {
	rootCmd := cli.Root()
	rootCmd.SetArgs([]string{"--output-format", "yaml", "--user-id", "invalid user", "env"})
	rootCmd.SetOut(&bytes.Buffer{})

	require.NoError(t, rootCmd.ExecuteContext(t.Context()))
}

func TestRootCommandInvokeContract(t *testing.T) {
	rootCmd := cli.Root()
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
	rootCmd := cli.Root()

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
	rootCmd := cli.Root()

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
	rootCmd := cli.Root()
	rootCmd.SetArgs(nil)
	rootCmd.SetIn(&bytes.Buffer{})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.ExecuteContext(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kagent requires a terminal")
	assert.Contains(t, err.Error(), "kagent invoke")
}
