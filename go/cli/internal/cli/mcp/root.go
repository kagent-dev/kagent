package mcp

import (
	"github.com/spf13/cobra"
)

// NewMCPCmd creates the root MCP command
func NewMCPCmd() *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server management",
		Long: `MCP server management commands for creating and managing
Model Context Protocol servers with dynamic tool loading.`,
	}

	mcpCmd.AddCommand(InitCmd)
	mcpCmd.AddCommand(BuildCmd)
	mcpCmd.AddCommand(DeployCmd)
	mcpCmd.AddCommand(AddToolCmd)
	mcpCmd.AddCommand(RunCmd)
	mcpCmd.AddCommand(SecretsCmd)

	return mcpCmd
}
