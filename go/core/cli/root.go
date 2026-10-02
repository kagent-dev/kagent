package cli

import (
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/mcp"
	sandboxcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/sandbox"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
)

// Root creates a fresh kagent command tree.
func Root() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "kagent",
		Short:         "kagent is a CLI for kagent",
		Long:          "kagent is a CLI for kagent",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runInteractive,
	}
	connection.RegisterFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().StringP(clioutput.FlagName, "o", string(clioutput.FormatTable), "Output format")

	rootCmd.AddCommand(
		commands.NewAgentCmd(),
		commands.NewApplyAgentCmd(),
		commands.NewInstallCmd(),
		commands.NewUninstallCmd(),
		commands.NewBugReportCmd(),
		commands.NewVersionCmd(),
		commands.NewDashboardCmd(),
		mcp.NewMCPCmd(),
		sandboxcli.NewCmd(),
		commands.NewEnvCmd(),
		dbcli.NewDBCmd(),
	)
	return rootCmd
}
