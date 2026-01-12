package mcp

import "github.com/spf13/cobra"

var ServeAgentsCmd = &cobra.Command{
	Use:   "serve-agents",
	Short: "Serve kagent agents via MCP",
}
