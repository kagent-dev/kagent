package mcp

import (
	"context"
	"os"

	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
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
		s.AddTool(mcp.NewTool("echo",
			mcp.WithDescription("Echo back the input message"),
			mcp.WithString("message", mcp.Description("Message to echo"), mcp.Required()),
		), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			message, err := request.RequireString("message")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(message), nil
		})
		stdioServer := mcpserver.NewStdioServer(s)
		return stdioServer.Listen(cmd.Context(), os.Stdin, os.Stdout)
	},
}
