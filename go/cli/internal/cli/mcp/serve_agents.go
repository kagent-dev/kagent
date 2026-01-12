package mcp

import (
	"github.com/kagent-dev/kagent/go/internal/version"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var ServeAgentsCmd = &cobra.Command{
	Use:   "serve-agents",
	Short: "Serve kagent agents via MCP",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := mcpserver.NewMCPServer(
			"kagent-agents",
			version.Version,
			mcpserver.WithToolCapabilities(false),
		)
		return mcpserver.ServeStdio(s)
	},
}
