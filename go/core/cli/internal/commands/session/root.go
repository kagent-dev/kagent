package session

import "github.com/spf13/cobra"

// NewCmd groups conversation discovery and lifecycle operations.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage agent conversations"}
	cmd.AddCommand(newCreateCmd(), newReadCmd(true), newReadCmd(false), newDeleteCmd(), newPushCmd())
	return cmd
}
