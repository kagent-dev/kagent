package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

var (
	serveAgentsTransport string
	serveAgentsHost      string
	serveAgentsPort      int
)

var a2aContextBySessionAndAgent sync.Map

var ServeAgentsCmd = &cobra.Command{
	Use:   "serve-agents",
	Short: "Serve kagent agents via MCP",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, cfgErr := config.Get()
		s := mcpserver.NewMCPServer(
			"kagent-agents",
			version.Version,
			mcpserver.WithToolCapabilities(false),
		)

		s.AddTool(mcp.NewTool("list_agents",
			mcp.WithDescription("List invokable kagent agents (accepted + deploymentReady)"),
		), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if cfgErr != nil {
				return mcp.NewToolResultErrorFromErr("config", cfgErr), nil
			}
			resp, err := cfg.Client().Agent.ListAgents(ctx)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("list agents", err), nil
			}
			type agentSummary struct {
				Ref         string `json:"ref"`
				Description string `json:"description,omitempty"`
			}
			agents := make([]agentSummary, 0)
			for _, agent := range resp.Data {
				if !agent.Accepted || !agent.DeploymentReady || agent.Agent == nil {
					continue
				}
				ref := agent.Agent.Namespace + "/" + agent.Agent.Name
				agents = append(agents, agentSummary{Ref: ref, Description: agent.Agent.Spec.Description})
			}
			result, err := mcp.NewToolResultJSON(agents)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("encode agents", err), nil
			}
			return result, nil
		})

		s.AddTool(mcp.NewTool("invoke_agent",
			mcp.WithDescription("Invoke a kagent agent via A2A"),
			mcp.WithString("agent", mcp.Description("Agent name (or namespace/name)"), mcp.Required()),
			mcp.WithString("task", mcp.Description("Task to run"), mcp.Required()),
		), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if cfgErr != nil {
				return mcp.NewToolResultErrorFromErr("config", cfgErr), nil
			}
			agentRef, err := request.RequireString("agent")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			task, err := request.RequireString("task")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			agentNS, agentName, ok := strings.Cut(agentRef, "/")
			if !ok {
				agentNS, agentName = cfg.Namespace, agentRef
			}
			agentRef = agentNS + "/" + agentName

			sessionID := "unknown"
			if session := mcpserver.ClientSessionFromContext(ctx); session != nil {
				sessionID = session.SessionID()
			}
			contextKey := sessionID + "|" + agentRef
			var contextIDPtr *string
			if prior, ok := a2aContextBySessionAndAgent.Load(contextKey); ok {
				if priorStr, ok := prior.(string); ok && priorStr != "" {
					contextIDPtr = &priorStr
				}
			}

			a2aURL := fmt.Sprintf("%s/api/a2a/%s/%s", cfg.KAgentURL, agentNS, agentName)
			client, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithTimeout(cfg.Timeout))
			if err != nil {
				return mcp.NewToolResultErrorFromErr("a2a client", err), nil
			}
			result, err := client.SendMessage(ctx, protocol.SendMessageParams{Message: protocol.Message{
				Kind: protocol.KindMessage, Role: protocol.MessageRoleUser, ContextID: contextIDPtr, Parts: []protocol.Part{protocol.NewTextPart(task)},
			}})
			if err != nil {
				return mcp.NewToolResultErrorFromErr("a2a send", err), nil
			}

			var responseText, newContextID string
			switch a2aResult := result.Result.(type) {
			case *protocol.Message:
				responseText = a2a.ExtractText(*a2aResult)
				if a2aResult.ContextID != nil {
					newContextID = *a2aResult.ContextID
				}
			case *protocol.Task:
				newContextID = a2aResult.ContextID
				if a2aResult.Status.Message != nil {
					responseText = a2a.ExtractText(*a2aResult.Status.Message)
				}
				for _, artifact := range a2aResult.Artifacts {
					responseText += a2a.ExtractText(protocol.Message{Parts: artifact.Parts})
				}
			}
			if responseText == "" {
				raw, err := result.MarshalJSON()
				if err != nil {
					return mcp.NewToolResultErrorFromErr("marshal result", err), nil
				}
				responseText = string(raw)
			}
			if newContextID != "" {
				a2aContextBySessionAndAgent.Store(contextKey, newContextID)
			}
			return mcp.NewToolResultStructured(map[string]any{
				"agent":      agentRef,
				"context_id": newContextID,
				"text":       responseText,
			}, responseText), nil
		})

		switch strings.ToLower(serveAgentsTransport) {
		case "stdio":
			stdioServer := mcpserver.NewStdioServer(s)
			return stdioServer.Listen(cmd.Context(), os.Stdin, os.Stdout)
		case "http":
			addr := fmt.Sprintf("%s:%d", serveAgentsHost, serveAgentsPort)
			httpServer := mcpserver.NewStreamableHTTPServer(s)
			go func() {
				<-cmd.Context().Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(shutdownCtx)
			}()
			if err := httpServer.Start(addr); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		default:
			return fmt.Errorf("invalid transport %q (expected stdio or http)", serveAgentsTransport)
		}
	},
}

func init() {
	ServeAgentsCmd.Flags().StringVar(&serveAgentsTransport, "transport", "stdio", "Transport mode (stdio or http)")
	ServeAgentsCmd.Flags().StringVar(&serveAgentsHost, "host", "127.0.0.1", "HTTP host to bind (when --transport http)")
	ServeAgentsCmd.Flags().IntVar(&serveAgentsPort, "port", 3000, "HTTP port to bind (when --transport http)")
}
