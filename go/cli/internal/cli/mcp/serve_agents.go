package mcp

import (
	"context"
	"os"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
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
		if cfg, err := config.Get(); err == nil {
			if resp, err := cfg.Client().Agent.ListAgents(cmd.Context()); err == nil {
				for _, agent := range resp.Data {
					if !agent.Accepted || !agent.DeploymentReady || agent.Agent == nil {
						continue
					}
					toolName, agentNS, agentName := agent.ID, agent.Agent.Namespace, agent.Agent.Name
					s.AddTool(mcp.NewTool(toolName,
						mcp.WithDescription("kagent agent "+agentNS+"/"+agentName),
						mcp.WithString("task", mcp.Description("Task to run"), mcp.Required()),
					), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						if _, err := request.RequireString("task"); err != nil {
							return mcp.NewToolResultError(err.Error()), nil
						}
						return mcp.NewToolResultError("agent tool not wired yet: " + agentNS + "/" + agentName), nil
					})
					break
				}
			}
		}
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
