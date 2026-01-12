package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
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
						mcp.WithString("context_id", mcp.Description("A2A context ID")),
						mcp.WithNumber("history_length", mcp.Description("Requested history length")),
						mcp.WithString("task", mcp.Description("Task to run"), mcp.Required()),
					), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						task, err := request.RequireString("task")
						if err != nil {
							return mcp.NewToolResultError(err.Error()), nil
						}
						contextID := request.GetString("context_id", "")
						historyLength := request.GetInt("history_length", 0)
						var contextIDPtr *string
						if contextID != "" {
							contextIDPtr = &contextID
						}
						var historyLengthPtr *int
						if historyLength > 0 {
							historyLengthPtr = &historyLength
						}
						a2aURL := fmt.Sprintf("%s/api/a2a/%s/%s", cfg.KAgentURL, agentNS, agentName)
						client, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithTimeout(cfg.Timeout))
						if err != nil {
							return mcp.NewToolResultErrorFromErr("a2a client", err), nil
						}
						params := protocol.SendMessageParams{
							Configuration: &protocol.SendMessageConfiguration{HistoryLength: historyLengthPtr},
							Message: protocol.Message{
								Kind: protocol.KindMessage, Role: protocol.MessageRoleUser, ContextID: contextIDPtr, Parts: []protocol.Part{protocol.NewTextPart(task)},
							},
						}
						result, err := client.SendMessage(ctx, params)
						if err != nil {
							return mcp.NewToolResultErrorFromErr("a2a send", err), nil
						}
						var text string
						switch a2aResult := result.Result.(type) {
						case *protocol.Message:
							text = a2a.ExtractText(*a2aResult)
						case *protocol.Task:
							if a2aResult.Status.Message != nil {
								text = a2a.ExtractText(*a2aResult.Status.Message)
							}
							for _, artifact := range a2aResult.Artifacts {
								text += a2a.ExtractText(protocol.Message{Parts: artifact.Parts})
							}
						}
						if text == "" {
							raw, err := result.MarshalJSON()
							if err != nil {
								return mcp.NewToolResultErrorFromErr("marshal result", err), nil
							}
							text = string(raw)
						}
						return mcp.NewToolResultText(text), nil
					})
				}
			}
		}
		stdioServer := mcpserver.NewStdioServer(s)
		return stdioServer.Listen(cmd.Context(), os.Stdin, os.Stdout)
	},
}
