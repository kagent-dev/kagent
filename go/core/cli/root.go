package cli

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	agentinstancecli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/agentinstance"
	agenttemplatecli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/agenttemplate"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/envdoc"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/mcp"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/spf13/cobra"
)

// Root creates a fresh kagent command tree.
func Root() *cobra.Command {
	connectionOptions := connection.DefaultOptions()
	cfg := &connectionOptions
	outputFormat := "table"
	rootCmd := &cobra.Command{
		Use:           "kagent",
		Short:         "kagent is a CLI for kagent",
		Long:          "kagent is a CLI for kagent",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInteractive(cmd, cfg)
		},
	}
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentURL, "kagent-url", cfg.KAgentURL, "KAgent REST URL")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCURL, "kagent-grpc-url", cfg.KAgentGRPCURL, "KAgent gRPC target")
	rootCmd.PersistentFlags().BoolVar(&cfg.KAgentGRPCTLS, "kagent-grpc-tls", cfg.KAgentGRPCTLS, "Use TLS for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCCAFile, "kagent-grpc-ca-file", cfg.KAgentGRPCCAFile, "CA certificate file for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCServerName, "kagent-grpc-server-name", cfg.KAgentGRPCServerName, "TLS server name for KAgent gRPC")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", cfg.Namespace, "Namespace")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "o", outputFormat, "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Verbose output")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "Timeout")
	rootCmd.PersistentFlags().StringVar(&cfg.UserID, "user-id", cfg.UserID, "Caller identity used to select the server-side data partition")
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get a kagent resource",
		Long:  `Get a kagent resource`,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}

	// Add subcommands to the respective parent commands
	getCmd.AddCommand(agentinstancecli.NewGetCmd(cfg, &outputFormat))
	getCmd.AddCommand(agenttemplatecli.NewGetCmd(&cfg.Namespace, &outputFormat))
	createCmd.AddCommand(agentinstancecli.NewCreateCmd(cfg, &outputFormat))
	deleteCmd.AddCommand(agentinstancecli.NewDeleteCmd(cfg, &outputFormat))
	rootCmd.AddCommand(commands.NewInstallCmd(cfg))
	rootCmd.AddCommand(commands.NewUninstallCmd(&cfg.Namespace))
	rootCmd.AddCommand(agentinstancecli.NewInvokeCmd(cfg, &outputFormat))
	rootCmd.AddCommand(commands.NewBugReportCmd(cfg))
	rootCmd.AddCommand(commands.NewVersionCmd(cfg))
	rootCmd.AddCommand(commands.NewDashboardCmd(&cfg.Namespace))
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(mcp.NewMCPCmd())
	rootCmd.AddCommand(envdoc.NewEnvCmd())
	rootCmd.AddCommand(dbcli.NewDBCmd(cfg))

	return rootCmd
}
