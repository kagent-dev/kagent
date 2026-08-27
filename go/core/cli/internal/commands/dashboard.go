package commands

import (
	"github.com/spf13/cobra"
)

// NewDashboardCmd constructs the kagent dashboard command.
func NewDashboardCmd(namespace *string) *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		Run: func(cmd *cobra.Command, _ []string) {
			runDashboard(cmd.Context(), *namespace)
		},
	}
}
