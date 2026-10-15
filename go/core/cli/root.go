package cli

import (
	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	sandboxcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/sandbox"
	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
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
	cliconfig.Register(rootCmd)

	rootCmd.AddCommand(
		commands.NewAgentCmd(),
		commands.NewApplyAgentCmd(),
		commands.NewInstallCmd(),
		commands.NewUninstallCmd(),
		commands.NewBugReportCmd(),
		commands.NewVersionCmd(),
		commands.NewDashboardCmd(),
		sandboxcli.NewCmd(),
		commands.NewEnvCmd(),
		dbcli.NewDBCmd(),
	)
	return rootCmd
}
