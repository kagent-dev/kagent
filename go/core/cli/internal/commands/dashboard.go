package commands

import (
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/spf13/cobra"
)

// NewDashboardCmd constructs the kagent dashboard command.
func NewDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			runDashboard(cmd.Context(), options.Namespace)
			return nil
		},
	}
}
