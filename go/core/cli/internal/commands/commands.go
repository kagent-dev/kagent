package commands

import (
	"fmt"
	"os"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/profiles"
	"github.com/spf13/cobra"
)

// NewInstallCommand constructs the kagent install command.
func NewInstallCommand(connectionOptions *connection.Options) *cobra.Command {
	cfg := &InstallCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install kagent",
		Long:  `Install kagent`,
		Run: func(cmd *cobra.Command, _ []string) {
			InstallCmd(cmd.Context(), cfg)
		},
	}
	cmd.Flags().StringVar(&cfg.Profile, "profile", "", "Installation profile (minimal|demo)")
	_ = cmd.RegisterFlagCompletionFunc("profile", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return profiles.Profiles, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// NewUninstallCommand constructs the kagent uninstall command.
func NewUninstallCommand(namespace *string) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall kagent",
		Long:  `Uninstall kagent`,
		Run: func(cmd *cobra.Command, _ []string) {
			UninstallCmd(cmd.Context(), *namespace)
		},
	}
}

// NewBugReportCommand constructs the kagent bug-report command.
func NewBugReportCommand(connectionOptions *connection.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "bug-report",
		Short: "Generate a bug report",
		Long:  `Generate a bug report`,
		Run: func(cmd *cobra.Command, _ []string) {
			portForward, err := connection.Connect(cmd.Context(), connectionOptions)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
				return
			}
			if portForward != nil {
				defer portForward.Stop()
			}
			BugReportCmd(connectionOptions.Namespace, connectionOptions.Verbose)
		},
	}
}

// NewVersionCommand constructs the kagent version command.
func NewVersionCommand(connectionOptions *connection.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the kagent version",
		Long:  `Print the kagent version`,
		Run: func(cmd *cobra.Command, _ []string) {
			clientSet := connectionOptions.Client()
			defer clientSet.Close() //nolint:errcheck
			defer VersionCmd(clientSet)

			if portForward, _ := connection.Connect(cmd.Context(), connectionOptions); portForward != nil {
				defer portForward.Stop()
			}
		},
	}
}

// NewDashboardCommand constructs the kagent dashboard command.
func NewDashboardCommand(namespace *string) *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		Run: func(cmd *cobra.Command, _ []string) {
			DashboardCmd(cmd.Context(), *namespace)
		},
	}
}
