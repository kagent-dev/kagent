package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/apimachinery/pkg/types"
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
	httpHandler   *mcpsdk.StreamableHTTPHandler
	server        *mcpsdk.Server
	a2aClients    sync.Map
}

// Input types for MCP tools
type ListAgentsInput struct{}

type ListAgentsOutput struct {
	Agents []AgentSummary `json:"agents"`
}

type AgentSummary struct {
	Ref         string `json:"ref"`
	Description string `json:"description,omitempty"`
}

type InvokeAgentInput struct {
	Agent     string `json:"agent" jsonschema:"Agent reference in format namespace/name"`
	Task      string `json:"task" jsonschema:"Task to run"`
	ContextID string `json:"context_id,omitempty" jsonschema:"Optional A2A context ID to continue a conversation"`
}

type InvokeAgentOutput struct {
	Agent     string `json:"agent"`
	Text      string `json:"text"`
	ContextID string `json:"context_id,omitempty"`
}

// NewMCPHandler creates a new MCP handler
// Wraps the StreamableHTTPHandler and adds A2A bridging and context management.
func NewMCPHandler(kubeClient client.Client, a2aBaseURL string, authenticator auth.AuthProvider) (*MCPHandler, error) {
	handler := &MCPHandler{
		kubeClient:    kubeClient,
		a2aBaseURL:    a2aBaseURL,
		authenticator: authenticator,
	}

	// Create MCP server
	impl := &mcpsdk.Implementation{
		Name:    "kagent-agents",
		Version: version.Version,
	}
	server := mcpsdk.NewServer(impl, nil)
	handler.server = server

	// Add list_agents tool
	mcpsdk.AddTool[ListAgentsInput, ListAgentsOutput](
		server,
		&mcpsdk.Tool{
			Name:        "list_agents",
			Description: "List invokable kagent agents (accepted + deploymentReady)",
		},
		handler.handleListAgents,
	)

	// Add invoke_agent tool
	mcpsdk.AddTool[InvokeAgentInput, InvokeAgentOutput](
		server,
		&mcpsdk.Tool{
			Name:        "invoke_agent",
			Description: "Invoke a kagent agent via A2A",
		},
		handler.handleInvokeAgent,
	)

	// Create HTTP handler
	handler.httpHandler = mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server {
			return server
		},
		nil,
	)

	return handler, nil
}

// handleListAgents handles the list_agents MCP tool
func (h *MCPHandler) handleListAgents(ctx context.Context, req *mcpsdk.CallToolRequest, input ListAgentsInput) (*mcpsdk.CallToolResult, ListAgentsOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "list_agents")

	agentList := &v1alpha2.AgentList{}
	if err := h.kubeClient.List(ctx, agentList); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list agents: %v", err)},
			},
			IsError: true,
		}, ListAgentsOutput{}, nil
	}

	agents := make([]AgentSummary, 0)
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
		agents = append(agents, AgentSummary{
			Ref:         ref,
			Description: description,
		})
	}

	log.Info("Listed agents", "count", len(agents))

	output := ListAgentsOutput{Agents: agents}

	var fallbackText strings.Builder
	if len(agents) == 0 {
		fallbackText.WriteString("No invokable agents found.")
	} else {
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
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fallbackText.String()},
		},
	}, output, nil
}

// handleInvokeAgent handles the invoke_agent MCP tool
func (h *MCPHandler) handleInvokeAgent(ctx context.Context, req *mcpsdk.CallToolRequest, input InvokeAgentInput) (*mcpsdk.CallToolResult, InvokeAgentOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "invoke_agent")

	// Parse agent reference (namespace/name or just name)
	agentNS, agentName, ok := strings.Cut(input.Agent, "/")
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: "agent must be in format 'namespace/name'"},
			},
			IsError: true,
		}, InvokeAgentOutput{}, nil
	}
	agentRef := agentNS + "/" + agentName
	agentNns := types.NamespacedName{Namespace: agentNS, Name: agentName}

	// Get context ID from client request (stateless mode)
	// If not provided, contextIDPtr will be nil and a new conversation will start
	var contextIDPtr *string
	if input.ContextID != "" {
		contextIDPtr = &input.ContextID
		log.V(1).Info("Using context_id from client request", "context_id", input.ContextID)
	}

	// Get or create cached A2A client for this agent
	a2aURL := fmt.Sprintf("%s/%s/", h.a2aBaseURL, agentRef)
	var a2aClient *a2aclient.A2AClient

	if cached, ok := h.a2aClients.Load(agentRef); ok {
		if client, ok := cached.(*a2aclient.A2AClient); ok {
			a2aClient = client
		}
	}

	// Create new client if not cached
	if a2aClient == nil {
		// Build A2A client options with authentication propagation
		a2aOpts := []a2aclient.Option{
			a2aclient.WithTimeout(30 * time.Second),
			a2aclient.WithHTTPReqHandler(
				authimpl.A2ARequestHandler(
					h.authenticator,
					agentNns,
				),
			),
		}

		newClient, err := a2aclient.NewA2AClient(a2aURL, a2aOpts...)
		if err != nil {
			log.Error(err, "Failed to create A2A client", "agent", agentRef)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to create A2A client: %v", err)},
				},
				IsError: true,
			}, InvokeAgentOutput{}, nil
		}

		// Cache the client
		h.a2aClients.Store(agentRef, newClient)
		a2aClient = newClient
	}

	// Send message via A2A
	result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:      protocol.KindMessage,
			Role:      protocol.MessageRoleUser,
			ContextID: contextIDPtr,
			Parts:     []protocol.Part{protocol.NewTextPart(input.Task)},
		},
	})
	if err != nil {
		log.Error(err, "Failed to send A2A message", "agent", agentRef)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to send A2A message: %v", err)},
			},
			IsError: true,
		}, InvokeAgentOutput{}, nil
	}

	// Extract response text and context ID
	var responseText, newContextID string
	switch a2aResult := result.Result.(type) {
	case *protocol.Message:
		responseText = a2a.ExtractText(*a2aResult)
		if a2aResult.ContextID != nil {
			newContextID = *a2aResult.ContextID
		}
	// Kagent A2A only returns Task type for now
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
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to marshal result: %v", err)},
				},
				IsError: true,
			}, InvokeAgentOutput{}, nil
		}
		responseText = string(raw)
	}

	log.Info("Invoked agent", "agent", agentRef, "hasContextID", newContextID != "")

	// Return context_id in response so client can store it for stateless operation
	output := InvokeAgentOutput{
		Agent: agentRef,
		Text:  responseText,
	}
	if newContextID != "" {
		output.ContextID = newContextID
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: responseText},
		},
	}, output, nil
}

// ServeHTTP implements http.Handler interface
func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The MCP HTTP handler handles all the routing internally
	h.httpHandler.ServeHTTP(w, r)
}

// Shutdown gracefully shuts down the MCP handler
func (h *MCPHandler) Shutdown(ctx context.Context) error {
	// The new SDK doesn't have an explicit Shutdown method on StreamableHTTPHandler
	// The server will be shut down when the context is cancelled
	return nil
}
