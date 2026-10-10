package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandUsesDefaultFlagValues(t *testing.T) {
	rootCmd := cli.Root()

	assert.Equal(t, "http://localhost:8083", rootCmd.PersistentFlags().Lookup("api-url").DefValue)
	assert.Equal(t, "http://localhost:8083", rootCmd.PersistentFlags().Lookup("gateway-url").DefValue)
	assert.Empty(t, rootCmd.PersistentFlags().Lookup("ca-file").DefValue)
	assert.Empty(t, rootCmd.PersistentFlags().Lookup("server-name").DefValue)
	assert.Equal(t, "kagent", rootCmd.PersistentFlags().Lookup("namespace").DefValue)
	assert.Equal(t, "table", rootCmd.PersistentFlags().Lookup("output-format").DefValue)
	assert.Equal(t, "false", rootCmd.PersistentFlags().Lookup("verbose").DefValue)
	assert.Equal(t, "5m0s", rootCmd.PersistentFlags().Lookup("timeout").DefValue)
	assert.Equal(t, "admin@kagent.dev", rootCmd.PersistentFlags().Lookup("user-id").DefValue)
}

func TestRootCommandFlagsOverrideOptionValues(t *testing.T) {
	rootCmd := cli.Root()
	require.NoError(t, rootCmd.ParseFlags([]string{
		"--api-url", "https://api.example.test",
		"--gateway-url", "https://gateway.example.test",
		"--ca-file", "/tmp/flag-ca.pem",
		"--server-name", "api.example.test",
		"--namespace", "flag-ns",
		"--output-format", "yaml",
		"--verbose",
		"--timeout", "10s",
		"--user-id", "flag-user",
	}))

	want := map[string]string{
		"api-url":       "https://api.example.test",
		"gateway-url":   "https://gateway.example.test",
		"ca-file":       "/tmp/flag-ca.pem",
		"server-name":   "api.example.test",
		"namespace":     "flag-ns",
		"output-format": "yaml",
		"verbose":       "true",
		"timeout":       "10s",
		"user-id":       "flag-user",
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

	invokeCmd, _, err := rootCmd.Find([]string{"agent", "invoke"})
	require.NoError(t, err)
	for _, flag := range []string{"session", "task", "file", "stream", "token"} {
		assert.NotNil(t, invokeCmd.Flags().Lookup(flag), "missing --%s", flag)
	}
	for _, legacyFlag := range []string{"agent", "url-override"} {
		assert.Nil(t, invokeCmd.Flags().Lookup(legacyFlag), "legacy --%s must be removed", legacyFlag)
	}

	listSessionCmd, _, err := rootCmd.Find([]string{"agent", "session", "list"})
	require.NoError(t, err)
	assert.Equal(t, "list", listSessionCmd.Use)
	for _, flag := range []string{"page-size", "page-token"} {
		assert.NotNil(t, listSessionCmd.Flags().Lookup(flag), "missing --%s", flag)
	}
}

func TestRootCommandV2CatalogAndLifecycleContract(t *testing.T) {
	rootCmd := cli.Root()

	listTemplateCmd, _, err := rootCmd.Find([]string{"agent", "template", "list"})
	require.NoError(t, err)
	assert.Equal(t, "list", listTemplateCmd.Use)
	for _, flag := range []string{"page-size", "page-token"} {
		assert.NotNil(t, listTemplateCmd.Flags().Lookup(flag), "missing --%s", flag)
	}

	createSessionCmd, _, err := rootCmd.Find([]string{"agent", "session", "create"})
	require.NoError(t, err)
	assert.Equal(t, "create", createSessionCmd.Use)
	for _, flag := range []string{"agent", "request-id"} {
		assert.NotNil(t, createSessionCmd.Flags().Lookup(flag), "missing --%s", flag)
	}

	deleteSessionCmd, _, err := rootCmd.Find([]string{"agent", "session", "delete"})
	require.NoError(t, err)
	assert.Equal(t, "delete ID", deleteSessionCmd.Use)

	applyCmd, _, err := rootCmd.Find([]string{"apply"})
	require.NoError(t, err)
	assert.Equal(t, "apply -f FILE", applyCmd.Use)
	assert.NotNil(t, applyCmd.Flags().Lookup("file"))
	sessionCmd, _, err := rootCmd.Find([]string{"agent", "session"})
	require.NoError(t, err)
	var sessionCommands []string
	for _, command := range sessionCmd.Commands() {
		sessionCommands = append(sessionCommands, command.Name())
	}
	assert.ElementsMatch(t, []string{"create", "list", "get", "delete", "push"}, sessionCommands)
	for _, operation := range []string{"create", "get", "list", "delete"} {
		_, _, err := rootCmd.Find([]string{"agent", "session", "push", operation})
		assert.NoError(t, err)
	}
	invokeCmd, _, err := rootCmd.Find([]string{"agent", "invoke"})
	require.NoError(t, err)
	assert.NotNil(t, invokeCmd.Flags().Lookup("push-url"))
	assert.NotNil(t, invokeCmd.Flags().Lookup("push-id"))
}

func TestRootCommandRemovesLegacyPaths(t *testing.T) {
	rootCmd := cli.Root()

	rootCommands := make([]string, 0, len(rootCmd.Commands()))
	for _, command := range rootCmd.Commands() {
		rootCommands = append(rootCommands, command.Name())
	}
	for _, command := range []string{"deploy", "init", "build", "run", "add-mcp", "get", "create", "delete", "invoke", "session"} {
		assert.NotContains(t, rootCommands, command)
	}
	assert.NotContains(t, rootCommands, "update")
	assert.Contains(t, rootCommands, "mcp")

	agentCmd, _, err := rootCmd.Find([]string{"agent"})
	require.NoError(t, err)
	agentCommands := make([]string, 0, len(agentCmd.Commands()))
	for _, command := range agentCmd.Commands() {
		agentCommands = append(agentCommands, command.Name())
	}
	assert.ElementsMatch(t, []string{"list", "get", "invoke", "session", "template"}, agentCommands)
}

func TestRootCommandRequiresTerminalForInteractiveUse(t *testing.T) {
	rootCmd := cli.Root()
	rootCmd.SetArgs(nil)
	rootCmd.SetIn(&bytes.Buffer{})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.ExecuteContext(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kagent requires a terminal")
	assert.Contains(t, err.Error(), "kagent agent invoke")
}

func TestRootCommandOutputFormatReachesResourceCommands(t *testing.T) {
	// An unparseable format is rejected before any command connects, so this
	// reaches the run function without touching the network or a cluster.
	for name, args := range map[string][]string{
		"list agents":    {"agent", "list"},
		"get agent":      {"agent", "get", "example"},
		"list templates": {"agent", "template", "list"},
		"get template":   {"agent", "template", "get", "example"},
		"list sessions":  {"agent", "session", "list"},
		"get session":    {"agent", "session", "get", "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"},
		"create session": {"agent", "session", "create", "--agent", "example"},
		"apply template": {"apply", "--file", "template.yaml"},
		"delete session": {"agent", "session", "delete", "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"},
		"invoke":         {"agent", "invoke", "--session", "8bd650a8-9775-488f-8bc1-0d52bf7bdcab", "--task", "hello"},
	} {
		t.Run(name, func(t *testing.T) {
			rootCmd := cli.Root()
			rootCmd.SetArgs(append(args, "--output-format", "bogus"))
			rootCmd.SetOut(&bytes.Buffer{})
			rootCmd.SetErr(&bytes.Buffer{})

			err := rootCmd.ExecuteContext(t.Context())

			require.Error(t, err)
			assert.Contains(t, err.Error(), `unsupported output format "bogus"`)
		})
	}
}

func TestAgentReadCommandsRequireExplicitTargets(t *testing.T) {
	for _, path := range [][]string{{"agent"}, {"agent", "template"}, {"agent", "session"}} {
		for _, tt := range []struct {
			name string
			args []string
			want string
		}{
			{name: "missing get target", args: []string{"get"}, want: "accepts 1 arg(s), received 0"},
			{name: "empty get target", args: []string{"get", ""}, want: "must not be empty"},
			{name: "multiple get targets", args: []string{"get", "one", "two"}, want: "accepts 1 arg(s), received 2"},
			{name: "list with target", args: []string{"list", "one"}, want: "unknown command"},
			{name: "pagination on get", args: []string{"get", "one", "--page-size", "2"}, want: "unknown flag: --page-size"},
		} {
			t.Run(strings.Join(path, " ")+"/"+tt.name, func(t *testing.T) {
				rootCmd := cli.Root()
				rootCmd.SetArgs(append(append([]string{}, path...), tt.args...))
				rootCmd.SetOut(&bytes.Buffer{})
				rootCmd.SetErr(&bytes.Buffer{})
				err := rootCmd.ExecuteContext(t.Context())
				require.ErrorContains(t, err, tt.want)
			})
		}
	}
}
