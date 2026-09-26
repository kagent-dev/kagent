package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listToolName            = "list_sessions"
	invokeToolName          = "invoke_session"
	defaultTaskPollInterval = 1000
)

type Handler struct {
	sessions    *sessionsvc.Service
	checkpoints *checkpoint.Service
	gateway     a2asrv.RequestHandler
	http        http.Handler
}

type ListSessionsInput struct {
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Maximum number of Sessions to return"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
}

type SessionSummary struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	State string `json:"state"`
}

type ListSessionsOutput struct {
	Sessions      []SessionSummary `json:"sessions"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

type InvokeSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"Session UUID"`
	Message   string `json:"message" jsonschema:"Message to send to the agent"`
	MessageID string `json:"message_id,omitempty" jsonschema:"Optional stable A2A message ID for idempotency"`
}

type InvokeSessionOutput struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	ContextID string `json:"context_id"`
	State     string `json:"state"`
	Text      string `json:"text,omitempty"`
}

func New(sessions *sessionsvc.Service, checkpoints *checkpoint.Service, gateway a2asrv.RequestHandler) (*Handler, error) {
	if sessions == nil || checkpoints == nil || gateway == nil {
		return nil, fmt.Errorf("session service, checkpoint service, and A2A gateway are required")
	}
	h := &Handler{sessions: sessions, checkpoints: checkpoints, gateway: gateway}
	capabilities := &mcp.ServerCapabilities{}
	capabilities.AddExtension(tasksExtension, nil)
	server := mcp.NewServer(
		&mcp.Implementation{Name: "kagent", Version: version.Version},
		&mcp.ServerOptions{Capabilities: capabilities},
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        listToolName,
		Description: "List ready Sessions visible to the caller",
	}, h.listSessions)
	mcp.AddTool(server, &mcp.Tool{
		Name:        invokeToolName,
		Description: "Invoke a Session through the public A2A gateway",
	}, h.invokeSession)
	h.registerCheckpointTools(server)
	server.AddReceivingMiddleware(h.taskAwareToolCall)
	if err := h.registerTaskMethods(server); err != nil {
		return nil, err
	}
	h.http = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.http.ServeHTTP(w, r)
}

func (h *Handler) listSessions(ctx context.Context, _ *mcp.CallToolRequest, input ListSessionsInput) (*mcp.CallToolResult, ListSessionsOutput, error) {
	result, err := h.sessions.List(ctx, sessionsvc.ListRequest{
		PageSize: input.PageSize, PageToken: input.PageToken,
	})
	if err != nil {
		return toolError(err), ListSessionsOutput{}, nil
	}
	output := ListSessionsOutput{Sessions: []SessionSummary{}, NextPageToken: result.NextPageToken}
	for _, session := range result.Sessions {
		if session.GetState() != apiv1alpha1.SessionState_SESSION_STATE_READY {
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
	}
	if text.Len() == 0 {
		text.WriteString("No ready Sessions found.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}, output, nil
}

func (h *Handler) invokeSession(ctx context.Context, _ *mcp.CallToolRequest, input InvokeSessionInput) (*mcp.CallToolResult, InvokeSessionOutput, error) {
	task, err := h.invoke(ctx, input, false)
	if err != nil {
		return toolError(err), InvokeSessionOutput{}, nil
	}
	result, output := invocationResult(input, task)
	return result, output, nil
}

func (h *Handler) invoke(ctx context.Context, input InvokeSessionInput, async bool) (*a2atype.Task, error) {
	if input.SessionID == "" || strings.TrimSpace(input.Message) == "" {
		return nil, fmt.Errorf("session_id and message are required")
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(input.Message))
	if input.MessageID != "" {
		message.ID = input.MessageID
	}
	session, err := h.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}
	message.ContextID = session.GetId()
	// Direct gateway calls carry the Agent tenant on the request, like A2A transports do.
	tenant := session.GetAgent().GetNamespace() + "/" + session.GetAgent().GetName()
	routed := interactionContext(ctx)
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

func invocationResult(input InvokeSessionInput, task *a2atype.Task) (*mcp.CallToolResult, InvokeSessionOutput) {
	text := taskText(task)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: task.Status.State == a2atype.TaskStateFailed ||
			task.Status.State == a2atype.TaskStateRejected ||
			task.Status.State == a2atype.TaskStateAuthRequired,
	}, InvokeSessionOutput{
		SessionID: input.SessionID,
		TaskID:    string(task.ID), ContextID: task.ContextID,
		State: task.Status.State.String(), Text: text,
	}
}

func taskText(task *a2atype.Task) string {
	var text strings.Builder
	if task.Status.Message != nil {
		text.WriteString(a2a.ExtractText(task.Status.Message))
	}
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part != nil {
				text.WriteString(part.Text())
			}
		}
	}
	if text.Len() == 0 {
		for _, message := range slices.Backward(task.History) {
			if message.Role == a2atype.MessageRoleAgent {
				text.WriteString(a2a.ExtractText(message))
				break
			}
		}
	}
	if text.Len() == 0 {
		data, _ := json.Marshal(task)
		return string(data)
	}
	return text.String()
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
