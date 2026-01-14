package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// MCPHandler handles MCP requests and bridges them to A2A endpoints
type MCPHandler struct {
	kubeClient    client.Client
	a2aBaseURL    string
	authenticator auth.AuthProvider
	httpServer    *mcpserver.StreamableHTTPServer
	lock          sync.RWMutex
	// Map to store context IDs per session and agent
	contextBySessionAndAgent sync.Map
}

// NewMCPHandler creates a new MCP handler
// Wraps the StreamableHTTPServer handler adds A2A bridging and context management.
func NewMCPHandler(kubeClient client.Client, a2aBaseURL string, authenticator auth.AuthProvider) (*MCPHandler, error) {
	handler := &MCPHandler{
		kubeClient:    kubeClient,
		a2aBaseURL:    a2aBaseURL,
		authenticator: authenticator,
	}

	// Create MCP server with tools and session cleanup hooks
	hooks := &mcpserver.Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, session mcpserver.ClientSession) {
		sessionID := session.SessionID()
		handler.contextBySessionAndAgent.Range(func(key, _ any) bool {
			keyStr, ok := key.(string)
			if !ok {
				return true
			}
			if strings.HasPrefix(keyStr, sessionID+"|") {
				handler.contextBySessionAndAgent.Delete(key)
			}
			return true
		})
	})

	s := mcpserver.NewMCPServer(
		"kagent-agents",
		version.Version,
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithHooks(hooks),
	)

	// Add list_agents tool
	s.AddTool(mcp.NewTool("list_agents",
		mcp.WithDescription("List invokable kagent agents (accepted + deploymentReady)"),
	), handler.handleListAgents)

	// Add invoke_agent tool
	s.AddTool(mcp.NewTool("invoke_agent",
		mcp.WithDescription("Invoke a kagent agent via A2A"),
		mcp.WithString("agent", mcp.Description("Agent name (or namespace/name)"), mcp.Required()),
		mcp.WithString("task", mcp.Description("Task to run"), mcp.Required()),
	), handler.handleInvokeAgent)

	// Create HTTP server
	handler.httpServer = mcpserver.NewStreamableHTTPServer(s)

	return handler, nil
}

// handleListAgents handles the list_agents MCP tool
func (h *MCPHandler) handleListAgents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "list_agents")

	agentList := &v1alpha2.AgentList{}
	if err := h.kubeClient.List(ctx, agentList); err != nil {
		return mcp.NewToolResultErrorFromErr("list agents", err), nil
	}

	type agentSummary struct {
		Ref         string `json:"ref"`
		Description string `json:"description,omitempty"`
	}

	agents := make([]agentSummary, 0)
	for _, agent := range agentList.Items {
		// Check if agent is accepted and deployment ready
		deploymentReady := false
		accepted := false
		for _, condition := range agent.Status.Conditions {
			if condition.Type == "Ready" && condition.Reason == "DeploymentReady" && condition.Status == "True" {
				deploymentReady = true
			}
			if condition.Type == "Accepted" && condition.Status == "True" {
				accepted = true
			}
		}

		if !accepted || !deploymentReady {
			continue
		}

		ref := agent.Namespace + "/" + agent.Name
		description := agent.Spec.Description
		agents = append(agents, agentSummary{
			Ref:         ref,
			Description: description,
		})
	}

	log.Info("Listed agents", "count", len(agents))
	if len(agents) == 0 {
		return mcp.NewToolResultStructured(map[string]any{"agents": agents}, "No invokable agents found."), nil
	}

	var fallbackText strings.Builder
	for i, agent := range agents {
		if i > 0 {
			fallbackText.WriteByte('\n')
		}
		fallbackText.WriteString(agent.Ref)
		if agent.Description != "" {
			fallbackText.WriteString(" - ")
			fallbackText.WriteString(agent.Description)
		}
	}

	return mcp.NewToolResultStructured(map[string]any{"agents": agents}, fallbackText.String()), nil
}

// handleInvokeAgent handles the invoke_agent MCP tool
func (h *MCPHandler) handleInvokeAgent(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "invoke_agent")

	agentRef, err := request.RequireString("agent")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	task, err := request.RequireString("task")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Parse agent reference (namespace/name or just name)
	agentNS, agentName, ok := strings.Cut(agentRef, "/")
	if !ok {
		return mcp.NewToolResultError("agent must be in format 'namespace/name'"), nil
	}
	agentRef = agentNS + "/" + agentName

	// Get session ID from context if available
	sessionID := ""
	if session := mcpserver.ClientSessionFromContext(ctx); session != nil {
		sessionID = session.SessionID()
	} else if headerSessionID := request.Header.Get(mcpserver.HeaderKeySessionID); headerSessionID != "" {
		sessionID = headerSessionID
	}
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Get or create context ID for this session and agent
	contextKey := sessionID + "|" + agentRef
	var contextIDPtr *string
	if prior, ok := h.contextBySessionAndAgent.Load(contextKey); ok {
		if priorStr, ok := prior.(string); ok && priorStr != "" {
			contextIDPtr = &priorStr
		}
	}

	// Create A2A client
	a2aURL := fmt.Sprintf("%s/%s/", h.a2aBaseURL, agentRef)
	a2aClient, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithTimeout(30*time.Second))
	if err != nil {
		log.Error(err, "Failed to create A2A client", "agent", agentRef)
		return mcp.NewToolResultErrorFromErr("a2a client", err), nil
	}

	// Send message via A2A
	result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:      protocol.KindMessage,
			Role:      protocol.MessageRoleUser,
			ContextID: contextIDPtr,
			Parts:     []protocol.Part{protocol.NewTextPart(task)},
		},
	})
	if err != nil {
		log.Error(err, "Failed to send A2A message", "agent", agentRef)
		return mcp.NewToolResultErrorFromErr("a2a send", err), nil
	}

	// Extract response text and context ID
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

	// Store new context ID if available
	if newContextID != "" {
		h.contextBySessionAndAgent.Store(contextKey, newContextID)
	}

	log.Info("Invoked agent", "agent", agentRef, "hasContextID", newContextID != "")
	return mcp.NewToolResultStructured(map[string]any{
		"agent": agentRef,
		"text":  responseText,
	}, responseText), nil
}

// ServeHTTP implements http.Handler interface
func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The MCP HTTP server handles all the routing internally
	h.httpServer.ServeHTTP(w, r)
}

// Shutdown gracefully shuts down the MCP handler
func (h *MCPHandler) Shutdown(ctx context.Context) error {
	return h.httpServer.Shutdown(ctx)
}
