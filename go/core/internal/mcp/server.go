package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

const (
	listToolName            = "list_sessions"
	invokeToolName          = "invoke_agent"
	defaultTaskPollInterval = 1000
)

type Handler struct {
	agents      *kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList]
	sessions    *sessionsvc.Service
	checkpoints *checkpoint.Service
	gateway     a2asrv.RequestHandler
	http        http.Handler
}

type Config struct {
	Agents           *kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList]
	Sessions         *sessionsvc.Service
	Checkpoints      *checkpoint.Service
	Gateway          a2asrv.RequestHandler
	Sandboxes        *sandbox.Service
	SandboxTemplates *kubecrud.Service[*v1alpha3.SandboxTemplate, *v1alpha3.SandboxTemplateList]
	// Extensions adds tools and resources after core has registered its own.
	Extensions func(*mcp.Server) error
}

type ListSessionsInput struct {
	PageSize  int             `json:"page_size,omitempty" jsonschema:"Maximum number of Sessions to return"`
	PageToken string          `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
	Agent     *agentReference `json:"agent,omitempty" jsonschema:"Only Sessions of this Agent"`
	AllStates bool            `json:"all_states,omitempty" jsonschema:"Include Sessions in every state, failed and suspended ones too; default lists ready Sessions only"`
}

// SessionSummary is a Session as every session tool answers it. Agent is
// namespace/name.
type SessionSummary struct {
	ID        string          `json:"id"`
	Agent     string          `json:"agent"`
	Name      string          `json:"name,omitempty"`
	State     string          `json:"state"`
	Operation string          `json:"operation,omitempty"`
	Failure   *FailureSummary `json:"failure,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

type ListSessionsOutput struct {
	Sessions      []SessionSummary `json:"sessions"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

type InvokeAgentInput struct {
	SessionID string          `json:"session_id,omitempty" jsonschema:"Existing Session UUID; provide either session_id or agent"`
	Agent     *agentReference `json:"agent,omitempty" jsonschema:"Agent to start a new Session with; omit session_id"`
	Message   string          `json:"message" jsonschema:"Message to send to the agent"`
	MessageID string          `json:"message_id,omitempty" jsonschema:"Stable A2A message ID; reuse with the same agent and message to retry creating a Session without another first turn"`
}

type agentReference struct {
	Namespace string `json:"namespace" jsonschema:"Agent namespace"`
	Name      string `json:"name" jsonschema:"Agent name"`
}

type InvokeAgentOutput struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	ContextID string `json:"context_id"`
	State     string `json:"state"`
	Text      string `json:"text,omitempty"`
}

func New(cfg Config) (*Handler, error) {
	if cfg.Agents == nil || cfg.Sessions == nil || cfg.Checkpoints == nil || cfg.Gateway == nil {
		return nil, fmt.Errorf("agent service, session service, checkpoint service, and A2A gateway are required")
	}
	h := &Handler{agents: cfg.Agents, sessions: cfg.Sessions, checkpoints: cfg.Checkpoints, gateway: cfg.Gateway}
	capabilities := &mcp.ServerCapabilities{}
	capabilities.AddExtension(tasksExtension, nil)
	instructions := "Use list_agents to discover Agents, then invoke_agent with an agent reference to start a conversation or session_id to continue one. Use checkpoint tools to save and fork Session state."
	if cfg.Sandboxes != nil {
		instructions += "\n\n" + sandboxInstructions
	}
	server := mcp.NewServer(
		&mcp.Implementation{Name: "kagent", Version: version.Version},
		&mcp.ServerOptions{Capabilities: capabilities, Instructions: instructions},
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_agents",
		Description: "List Agents visible to the caller, optionally filtered by namespace. Pass an entry from agents as the agent argument to invoke_agent to start a Session. Listing does not imply readiness or permission to invoke.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.listAgents)
	mcp.AddTool(server, &mcp.Tool{
		Name:        listToolName,
		Description: "List Sessions visible to the caller, ready ones unless all_states is set",
		Annotations: readOnly(),
	}, h.listSessions)
	// No top-level oneOf: Gemini rejects it. invokeAgent enforces exactly one of session_id or agent.
	mcp.AddTool(server, &mcp.Tool{
		Name:        invokeToolName,
		Description: "Send a message to an existing Session with session_id, or start a new Session by providing agent instead. Returns the Session and task IDs. Reuse message_id with identical inputs when retrying creation.",
	}, h.invokeAgent)
	h.registerSessionTools(server)
	h.registerCheckpointTools(server)
	registerSandboxTools(server, cfg.Sandboxes, cfg.SandboxTemplates)
	if cfg.Sandboxes != nil {
		registerSandboxPrompts(server)
	}
	server.AddReceivingMiddleware(h.taskAwareToolCall)
	if err := h.registerTaskMethods(server); err != nil {
		return nil, err
	}
	if cfg.Extensions != nil {
		if err := cfg.Extensions(server); err != nil {
			return nil, fmt.Errorf("extend /mcp: %w", err)
		}
	}
	h.http = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.http.ServeHTTP(w, r)
}

func (h *Handler) listSessions(ctx context.Context, _ *mcp.CallToolRequest, input ListSessionsInput) (*mcp.CallToolResult, ListSessionsOutput, error) {
	request := sessionsvc.ListRequest{PageSize: input.PageSize, PageToken: input.PageToken}
	if input.Agent != nil {
		request.Agent = &apiv1.ResourceReference{Namespace: input.Agent.Namespace, Name: input.Agent.Name}
	}
	result, err := h.sessions.List(ctx, request)
	if err != nil {
		return toolError(err), ListSessionsOutput{}, nil
	}
	output := ListSessionsOutput{Sessions: []SessionSummary{}, NextPageToken: result.NextPageToken}
	for _, session := range result.Sessions {
		if !input.AllStates && session.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY {
			continue
		}
		output.Sessions = append(output.Sessions, sessionSummary(session))
	}
	var text strings.Builder
	for i, session := range output.Sessions {
		if i > 0 {
			text.WriteByte('\n')
		}
		fmt.Fprintf(&text, "%s (%s)", session.ID, session.Agent)
		if input.AllStates {
			fmt.Fprintf(&text, " %s", session.State)
		}
	}
	if text.Len() == 0 && input.AllStates {
		text.WriteString("No Sessions found.")
	} else if text.Len() == 0 {
		text.WriteString("No ready Sessions found.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}, output, nil
}

func (h *Handler) invokeAgent(ctx context.Context, _ *mcp.CallToolRequest, input InvokeAgentInput) (*mcp.CallToolResult, InvokeAgentOutput, error) {
	task, err := h.invoke(ctx, input, false)
	if err != nil {
		return sessionToolError(err), InvokeAgentOutput{}, nil
	}
	result, output := invocationResult(task)
	return result, output, nil
}

func (h *Handler) invoke(ctx context.Context, input InvokeAgentInput, async bool) (*a2atype.Task, error) {
	if (input.SessionID != "") == (input.Agent != nil) {
		return nil, fmt.Errorf("provide exactly one of session_id or agent")
	}
	if strings.TrimSpace(input.Message) == "" {
		return nil, fmt.Errorf("message is required")
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(input.Message))
	if input.MessageID != "" {
		message.ID = input.MessageID
	}
	var tenant string
	if input.Agent != nil {
		// An unset context lets A2A own creation and initial-message retries.
		tenant = input.Agent.Namespace + "/" + input.Agent.Name
	} else {
		session, err := h.sessions.Get(ctx, input.SessionID)
		if err != nil {
			return nil, err
		}
		message.ContextID = session.GetId()
		agent := session.GetAgent().GetRef()
		tenant = agent.GetNamespace() + "/" + agent.GetName()
	}
	// Direct gateway calls carry the Agent tenant on the request, like A2A transports do.
	routed := interactionContext(ctx)
	if origin := runtimeauth.Origin(ctx); !async && origin != nil {
		// Admission persists the dependency before dispatch, so the caller resumes
		// when the delegated task settles after a pause for input.
		routed = sessionsvc.WithDelegatingSession(routed, origin.SessionID)
	}
	var taskID a2atype.TaskID
	for event, err := range h.gateway.SendStreamingMessage(routed, &a2atype.SendMessageRequest{Tenant: tenant, Message: message}) {
		if err != nil {
			return nil, err
		}
		if event != nil && event.TaskInfo().TaskID != "" {
			taskID = event.TaskInfo().TaskID
			if async {
				break
			}
		}
	}
	if taskID == "" {
		return nil, fmt.Errorf("A2A runtime did not return a task")
	}
	// The runtime continues after this observer leaves. Public responses use
	// the persisted task, including the lifecycle publication boundary.
	return h.gateway.GetTask(routed, &a2atype.GetTaskRequest{Tenant: tenant, ID: taskID})
}

func interactionContext(ctx context.Context) context.Context {
	ctx, _ = a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {adka2a.HITLExtensionURI},
	}))
	return ctx
}

func invocationResult(task *a2atype.Task) (*mcp.CallToolResult, InvokeAgentOutput) {
	text := a2a.TaskText(task)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: task.Status.State == a2atype.TaskStateFailed ||
			task.Status.State == a2atype.TaskStateRejected ||
			task.Status.State == a2atype.TaskStateAuthRequired,
	}, InvokeAgentOutput{
		SessionID: task.ContextID,
		TaskID:    string(task.ID), ContextID: task.ContextID,
		State: task.Status.State.String(), Text: text,
	}
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
