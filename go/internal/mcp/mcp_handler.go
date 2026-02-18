package mcp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	dbpkg "github.com/kagent-dev/kagent/go/pkg/database"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const defaultUserID = "admin@kagent.dev"

// MCPHandler handles MCP requests and bridges them to A2A endpoints
type MCPHandler struct {
	kubeClient         client.Client
	a2aBaseURL         string
	authenticator      auth.AuthProvider
	dbClient           dbpkg.Client
	uiBaseURL          string
	httpHandler        *mcpsdk.StreamableHTTPHandler
	server             *mcpsdk.Server
	a2aClients         sync.Map
	sendA2AMessageFunc func(ctx context.Context, agentRef string, contextID *string, text string) error
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
func NewMCPHandler(kubeClient client.Client, a2aBaseURL string, authenticator auth.AuthProvider, dbClient dbpkg.Client, uiBaseURL string) (*MCPHandler, error) {
	handler := &MCPHandler{
		kubeClient:    kubeClient,
		a2aBaseURL:    a2aBaseURL,
		authenticator: authenticator,
		dbClient:      dbClient,
		uiBaseURL:     uiBaseURL,
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

	// Add create_session tool
	mcpsdk.AddTool[CreateSessionInput, CreateSessionOutput](
		server,
		&mcpsdk.Tool{
			Name:        "create_agent_session",
			Description: "Create a new session (visible in the UI) for an agent with an initial task",
		},
		handler.handleCreateSession,
	)

	// Add get_session_events tool
	mcpsdk.AddTool[GetSessionEventsInput, GetSessionEventsOutput](
		server,
		&mcpsdk.Tool{
			Name:        "get_agent_session_events",
			Description: "Retrieve the event history (conversation) of a session",
		},
		handler.handleGetSessionEvents,
	)

	// Add send_session_message tool
	mcpsdk.AddTool[SendSessionMessageInput, SendSessionMessageOutput](
		server,
		&mcpsdk.Tool{
			Name:        "send_agent_session_message",
			Description: "Send a new message to an existing session (as the user)",
		},
		handler.handleSendSessionMessage,
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

func (h *MCPHandler) getOrCreateA2AClient(agentRef string) (*a2aclient.A2AClient, error) {
	if cached, ok := h.a2aClients.Load(agentRef); ok {
		if client, ok := cached.(*a2aclient.A2AClient); ok {
			return client, nil
		}
	}

	agentNS, agentName, _ := strings.Cut(agentRef, "/")
	a2aURL := fmt.Sprintf("%s/%s/", h.a2aBaseURL, agentRef)
	a2aOpts := []a2aclient.Option{
		a2aclient.WithTimeout(30 * time.Second),
		a2aclient.WithHTTPReqHandler(
			authimpl.A2ARequestHandler(
				h.authenticator,
				types.NamespacedName{Namespace: agentNS, Name: agentName},
			),
		),
	}

	client, err := a2aclient.NewA2AClient(a2aURL, a2aOpts...)
	if err != nil {
		return nil, err
	}
	h.a2aClients.Store(agentRef, client)
	return client, nil
}

// sendA2AMessage sends a non-blocking A2A message to the agent. The A2A protocol's
// Blocking=false flag causes the server to return immediately with a Task in
// "submitted" or "working" state while the agent continues processing in the background.
func (h *MCPHandler) sendA2AMessage(ctx context.Context, agentRef string, contextID *string, text string) error {
	if h.sendA2AMessageFunc != nil {
		return h.sendA2AMessageFunc(ctx, agentRef, contextID, text)
	}

	a2aClient, err := h.getOrCreateA2AClient(agentRef)
	if err != nil {
		return fmt.Errorf("failed to create A2A client: %w", err)
	}

	blocking := false
	_, err = a2aClient.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:      protocol.KindMessage,
			Role:      protocol.MessageRoleUser,
			ContextID: contextID,
			Parts:     []protocol.Part{protocol.NewTextPart(text)},
		},
		Configuration: &protocol.SendMessageConfiguration{
			Blocking: &blocking,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send A2A message: %w", err)
	}
	return nil
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

	// Get context ID from client request (stateless mode)
	// If not provided, contextIDPtr will be nil and a new conversation will start
	var contextIDPtr *string
	if input.ContextID != "" {
		contextIDPtr = &input.ContextID
		log.V(1).Info("Using context_id from client request", "context_id", input.ContextID)
	}

	a2aClient, err := h.getOrCreateA2AClient(agentRef)
	if err != nil {
		log.Error(err, "Failed to create A2A client", "agent", agentRef)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to create A2A client: %v", err)},
			},
			IsError: true,
		}, InvokeAgentOutput{}, nil
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

type CreateSessionInput struct {
	Agent  string `json:"agent" jsonschema:"Agent reference in format namespace/name"`
	Task   string `json:"task" jsonschema:"Initial task/message for the session"`
	UserID string `json:"user_id,omitempty" jsonschema:"User ID to assign the session to (optional)"`
	Name   string `json:"name,omitempty" jsonschema:"Optional name for the session"`
}

type CreateSessionOutput struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	URL       string `json:"url,omitempty"`
}

// handleCreateSession handles the create_session MCP tool
func (h *MCPHandler) handleCreateSession(ctx context.Context, req *mcpsdk.CallToolRequest, input CreateSessionInput) (*mcpsdk.CallToolResult, CreateSessionOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "create_agent_session")

	// Parse agent reference (namespace/name or just name)
	_, agentName, ok := strings.Cut(input.Agent, "/")
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: "agent must be in format 'namespace/name'"},
			},
			IsError: true,
		}, CreateSessionOutput{}, nil
	}

	if input.UserID == "" {
		input.UserID = defaultUserID
	}

	// Verify agent exists
	agent, err := h.dbClient.GetAgent(utils.ConvertToPythonIdentifier(input.Agent))
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to find agent %s: %v", input.Agent, err)},
			},
			IsError: true,
		}, CreateSessionOutput{}, nil
	}

	// Create session
	sessionID := protocol.GenerateContextID()
	sessionName := input.Name
	if sessionName == "" {
		sessionName = fmt.Sprintf("Session with %s", agentName)
	}

	session := &dbpkg.Session{
		ID:      sessionID,
		Name:    &sessionName,
		UserID:  input.UserID,
		AgentID: &agent.ID,
	}

	if err := h.dbClient.StoreSession(session); err != nil {
		log.Error(err, "Failed to store session")
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to create session: %v", err)},
			},
			IsError: true,
		}, CreateSessionOutput{}, nil
	}

	log.Info("Created session", "sessionID", sessionID, "agent", input.Agent, "userID", input.UserID)

	if err := h.sendA2AMessage(ctx, input.Agent, &sessionID, input.Task); err != nil {
		log.Error(err, "Failed to dispatch task to agent", "sessionID", sessionID, "agent", input.Agent)
		url := fmt.Sprintf("%s/agents/%s/chat/%s", h.uiBaseURL, input.Agent, sessionID)
		msg := fmt.Sprintf("Session created (ID: %s) but failed to dispatch the initial message to the agent: %v. You can retry by calling send_agent_session_message with session_id=%s.", sessionID, err, sessionID)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: msg},
			},
			IsError: true,
		}, CreateSessionOutput{SessionID: sessionID, Message: msg, URL: url}, nil
	}

	message := fmt.Sprintf("Session created successfully. Session ID: %s", sessionID)
	url := fmt.Sprintf("%s/agents/%s/chat/%s", h.uiBaseURL, input.Agent, sessionID)

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fmt.Sprintf("%s\nURL: %s", message, url)},
		},
	}, CreateSessionOutput{SessionID: sessionID, Message: message, URL: url}, nil
}

type GetSessionEventsInput struct {
	SessionID string `json:"session_id" jsonschema:"ID of the session to retrieve events from"`
	UserID    string `json:"user_id,omitempty" jsonschema:"User ID who owns the session (optional, defaults to admin@kagent.dev)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of events to retrieve (default: 50)"`
}

type GetSessionEventsOutput struct {
	Events []SessionEvent `json:"events"`
}

type SessionEvent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Type    string `json:"type"` // e.g. "message", "tool_call", "tool_result"
}

// handleGetSessionEvents handles the get_session_events MCP tool
func (h *MCPHandler) handleGetSessionEvents(ctx context.Context, req *mcpsdk.CallToolRequest, input GetSessionEventsInput) (*mcpsdk.CallToolResult, GetSessionEventsOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "get_session_events")

	if input.UserID == "" {
		input.UserID = defaultUserID
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	// Fetch events from DB
	dbEvents, err := h.dbClient.ListEventsForSession(input.SessionID, input.UserID, dbpkg.QueryOptions{
		Limit:    limit,
		OrderAsc: false, // Descending order to get most recent first
	})
	if err != nil {
		log.Error(err, "Failed to list events for session", "sessionID", input.SessionID)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list events: %v", err)},
			},
			IsError: true,
		}, GetSessionEventsOutput{}, nil
	}

	// Reverse events to be in chronological order for the transcript
	slices.Reverse(dbEvents)

	events := make([]SessionEvent, 0, len(dbEvents))
	var sb strings.Builder

	for _, dbEvent := range dbEvents {
		msg, err := dbEvent.Parse()
		if err != nil {
			log.Error(err, "Failed to parse event data", "eventID", dbEvent.ID)
			continue
		}

		// Convert protocol.Message to SessionEvent
		content := a2a.ExtractText(msg)
		role := string(msg.Role)

		event := SessionEvent{
			Role:    role,
			Content: content,
			Type:    string(msg.Kind),
		}
		events = append(events, event)

		// Format for text output
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", strings.ToUpper(role), content))
	}

	if len(events) == 0 {
		sb.WriteString("No events found for this session.")
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: sb.String()},
		},
	}, GetSessionEventsOutput{Events: events}, nil
}

type SendSessionMessageInput struct {
	SessionID string `json:"session_id" jsonschema:"ID of the session to send message to"`
	UserID    string `json:"user_id,omitempty" jsonschema:"User ID who owns the session (optional, defaults to admin@kagent.dev)"`
	Content   string `json:"content" jsonschema:"Message content to send"`
}

type SendSessionMessageOutput struct {
	Message string `json:"message"`
}

// handleSendSessionMessage handles the send_session_message MCP tool
func (h *MCPHandler) handleSendSessionMessage(ctx context.Context, req *mcpsdk.CallToolRequest, input SendSessionMessageInput) (*mcpsdk.CallToolResult, SendSessionMessageOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "send_agent_session_message")

	if input.UserID == "" {
		input.UserID = defaultUserID
	}

	// Verify session exists
	session, err := h.dbClient.GetSession(input.SessionID, input.UserID)
	if err != nil {
		log.Error(err, "Failed to find session", "sessionID", input.SessionID)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to find session %s: %v", input.SessionID, err)},
			},
			IsError: true,
		}, SendSessionMessageOutput{}, nil
	}

	if session.AgentID != nil {
		agentRef := utils.ConvertToKubernetesIdentifier(*session.AgentID)
		if err := h.sendA2AMessage(ctx, agentRef, &session.ID, input.Content); err != nil {
			log.Error(err, "Failed to dispatch message to agent", "sessionID", session.ID, "agent", agentRef)
			msg := fmt.Sprintf("Failed to dispatch message to agent: %v. WARNING: do NOT resend the same message — it may have been partially delivered. Check the session events first.", err)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: msg},
				},
				IsError: true,
			}, SendSessionMessageOutput{Message: msg}, nil
		}
	}

	log.Info("Sent message to session", "sessionID", session.ID)

	message := "Message sent successfully."

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: message},
		},
	}, SendSessionMessageOutput{Message: message}, nil
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
