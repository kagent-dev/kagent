package cli

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	agentinstancecli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/agentinstance"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/mcp"
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

	getCmd := newResourceGroupCmd("get", "Get a kagent resource")
	createCmd := newResourceGroupCmd("create", "Create a kagent resource")
	deleteCmd := newResourceGroupCmd("delete", "Delete a kagent resource")

	getCmd.AddCommand(agentinstancecli.NewGetCmd())
	getCmd.AddCommand(commands.NewGetAgentTemplateCmd())
	createCmd.AddCommand(agentinstancecli.NewCreateCmd())
	deleteCmd.AddCommand(agentinstancecli.NewDeleteCmd())

	rootCmd.AddCommand(
		getCmd,
		createCmd,
		deleteCmd,
		commands.NewApplyAgentTemplateCmd(),
		agentinstancecli.NewInvokeCmd(),
		commands.NewInstallCmd(),
		commands.NewUninstallCmd(),
		commands.NewBugReportCmd(),
		commands.NewVersionCmd(),
		commands.NewDashboardCmd(),
		mcp.NewMCPCmd(),
		commands.NewEnvCmd(),
		dbcli.NewDBCmd(),
	)
	return rootCmd
}

// newResourceGroupCmd builds a parent command that only routes to resource subcommands.
func newResourceGroupCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resourceTypes := make([]string, 0, len(cmd.Commands()))
			for _, child := range cmd.Commands() {
				resourceTypes = append(resourceTypes, child.Name())
			}
			return fmt.Errorf("resource type is required; available resource types: %s", strings.Join(resourceTypes, ", "))
		},
	}
}
