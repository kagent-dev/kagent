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
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listToolName            = "list_agent_instances"
	invokeToolName          = "invoke_agent_instance"
	defaultTaskPollInterval = 1000
)

type Handler struct {
	instances   *agentinstance.Service
	checkpoints *checkpoint.Service
	gateway     a2asrv.RequestHandler
	http        http.Handler
}

type ListAgentInstancesInput struct {
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Maximum number of AgentInstances to return"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call"`
}

type AgentInstanceSummary struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	State string `json:"state"`
}

type ListAgentInstancesOutput struct {
	AgentInstances []AgentInstanceSummary `json:"agent_instances"`
	NextPageToken  string                 `json:"next_page_token,omitempty"`
}

type InvokeAgentInstanceInput struct {
	AgentInstanceID string `json:"agent_instance_id" jsonschema:"AgentInstance UUID"`
	Message         string `json:"message" jsonschema:"Message to send to the agent"`
	MessageID       string `json:"message_id,omitempty" jsonschema:"Optional stable A2A message ID for idempotency"`
}

type InvokeAgentInstanceOutput struct {
	AgentInstanceID string `json:"agent_instance_id"`
	TaskID          string `json:"task_id"`
	ContextID       string `json:"context_id"`
	State           string `json:"state"`
	Text            string `json:"text,omitempty"`
}

func New(instances *agentinstance.Service, checkpoints *checkpoint.Service, gateway a2asrv.RequestHandler) (*Handler, error) {
	if instances == nil || checkpoints == nil || gateway == nil {
		return nil, fmt.Errorf("AgentInstance service, checkpoint service, and A2A gateway are required")
	}
	h := &Handler{instances: instances, checkpoints: checkpoints, gateway: gateway}
	capabilities := &mcp.ServerCapabilities{}
	capabilities.AddExtension(tasksExtension, nil)
	server := mcp.NewServer(
		&mcp.Implementation{Name: "kagent", Version: version.Version},
		&mcp.ServerOptions{Capabilities: capabilities},
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        listToolName,
		Description: "List ready AgentInstances visible to the caller",
	}, h.listAgentInstances)
	mcp.AddTool(server, &mcp.Tool{
		Name:        invokeToolName,
		Description: "Invoke an AgentInstance through the public A2A gateway",
	}, h.invokeAgentInstance)
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

func (h *Handler) listAgentInstances(ctx context.Context, _ *mcp.CallToolRequest, input ListAgentInstancesInput) (*mcp.CallToolResult, ListAgentInstancesOutput, error) {
	result, err := h.instances.List(ctx, agentinstance.ListRequest{
		PageSize: input.PageSize, PageToken: input.PageToken,
	})
	if err != nil {
		return toolError(err), ListAgentInstancesOutput{}, nil
	}
	output := ListAgentInstancesOutput{AgentInstances: []AgentInstanceSummary{}, NextPageToken: result.NextPageToken}
	for _, instance := range result.Instances {
		if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
			continue
		}
		output.AgentInstances = append(output.AgentInstances, agentInstanceSummary(instance))
	}
	var text strings.Builder
	for i, instance := range output.AgentInstances {
		if i > 0 {
			text.WriteByte('\n')
		}
		fmt.Fprintf(&text, "%s (%s)", instance.ID, instance.Agent)
	}
	if text.Len() == 0 {
		text.WriteString("No ready AgentInstances found.")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}, output, nil
}

func (h *Handler) invokeAgentInstance(ctx context.Context, _ *mcp.CallToolRequest, input InvokeAgentInstanceInput) (*mcp.CallToolResult, InvokeAgentInstanceOutput, error) {
	task, err := h.invoke(ctx, input, false)
	if err != nil {
		return toolError(err), InvokeAgentInstanceOutput{}, nil
	}
	result, output := invocationResult(input, task)
	return result, output, nil
}

func (h *Handler) invoke(ctx context.Context, input InvokeAgentInstanceInput, async bool) (*a2atype.Task, error) {
	if input.AgentInstanceID == "" || strings.TrimSpace(input.Message) == "" {
		return nil, fmt.Errorf("agent_instance_id and message are required")
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(input.Message))
	if input.MessageID != "" {
		message.ID = input.MessageID
	}
	instance, err := h.instances.Get(ctx, input.AgentInstanceID)
	if err != nil {
		return nil, err
	}
	message.ContextID = instance.GetId()
	routed := routeContext(ctx, instance.GetAgent().GetNamespace()+"/"+instance.GetAgent().GetName())
	var taskID a2atype.TaskID
	for event, err := range h.gateway.SendStreamingMessage(routed, &a2atype.SendMessageRequest{Message: message}) {
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
	return h.gateway.GetTask(routed, &a2atype.GetTaskRequest{ID: taskID})
}

func routeContext(ctx context.Context, agent string) context.Context {
	ctx = a2atype.AttachTenant(ctx, agent)
	ctx, _ = a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {adka2a.HITLExtensionURI},
	}))
	return ctx
}

func invocationResult(input InvokeAgentInstanceInput, task *a2atype.Task) (*mcp.CallToolResult, InvokeAgentInstanceOutput) {
	text := taskText(task)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: task.Status.State == a2atype.TaskStateFailed ||
			task.Status.State == a2atype.TaskStateRejected ||
			task.Status.State == a2atype.TaskStateAuthRequired,
	}, InvokeAgentInstanceOutput{
		AgentInstanceID: input.AgentInstanceID,
		TaskID:          string(task.ID), ContextID: task.ContextID,
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
