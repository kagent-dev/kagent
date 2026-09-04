package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/a2agateway"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tasksExtension = "io.modelcontextprotocol/tasks"

type taskReference struct {
	Namespace  string `json:"namespace"`
	InstanceID string `json:"instanceId"`
	TaskID     string `json:"taskId"`
}

type taskFields struct {
	TaskID         string `json:"taskId"`
	Status         string `json:"status"`
	StatusMessage  string `json:"statusMessage,omitempty"`
	CreatedAt      string `json:"createdAt"`
	LastUpdatedAt  string `json:"lastUpdatedAt"`
	TTLMS          *int64 `json:"ttlMs"`
	PollIntervalMS int64  `json:"pollIntervalMs,omitempty"`
}

type createTaskResult struct {
	mcp.ResultBase
	taskFields
	ResultType string `json:"resultType"`
}

type getTaskParams struct {
	mcp.ParamsBase
	TaskID string `json:"taskId"`
}

type getTaskResult struct {
	mcp.ResultBase
	taskFields
	ResultType    string              `json:"resultType"`
	InputRequests mcp.InputRequestMap `json:"inputRequests,omitempty"`
	Result        *mcp.CallToolResult `json:"result,omitempty"`
}

type updateTaskParams struct {
	mcp.ParamsBase
	TaskID         string               `json:"taskId"`
	InputResponses mcp.InputResponseMap `json:"inputResponses"`
}

type completeTaskResult struct {
	mcp.ResultBase
	ResultType string `json:"resultType"`
}

type cancelTaskParams struct {
	mcp.ParamsBase
	TaskID string `json:"taskId"`
}

func (h *Handler) registerTaskMethods(server *mcp.Server) error {
	if err := mcp.AddReceivingCustomMethod(server, "tasks/get", h.getTask); err != nil {
		return fmt.Errorf("register tasks/get: %w", err)
	}
	if err := mcp.AddReceivingCustomMethod(server, "tasks/update", h.updateTask); err != nil {
		return fmt.Errorf("register tasks/update: %w", err)
	}
	if err := mcp.AddReceivingCustomMethod(server, "tasks/cancel", h.cancelTask); err != nil {
		return fmt.Errorf("register tasks/cancel: %w", err)
	}
	return nil
}

func (h *Handler) taskAwareToolCall(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, request)
		}
		req, ok := request.(*mcp.ServerRequest[*mcp.CallToolParamsRaw])
		if !ok || req.Params == nil || req.Params.Name != invokeToolName || !supportsTasks(req.ClientCapabilities()) {
			return next(ctx, method, request)
		}
		var input InvokeAgentInstanceInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError(fmt.Errorf("invalid invocation input: %w", err)), nil
		}
		task, err := h.invoke(ctx, input, true)
		if err != nil {
			return toolError(err), nil
		}
		taskRef := taskReference{
			Namespace: input.Namespace, InstanceID: input.AgentInstanceID,
			TaskID: string(task.ID),
		}
		ref, err := encodeTaskReference(taskRef)
		if err != nil {
			return nil, err
		}
		fields := taskToMCP(ref, task)
		return &createTaskResult{taskFields: fields, ResultType: "task"}, nil
	}
}

func supportsTasks(capabilities *mcp.ClientCapabilities) bool {
	if capabilities == nil {
		return false
	}
	_, ok := capabilities.Extensions[tasksExtension]
	return ok
}

func taskParamsSupported(meta map[string]any) bool {
	data, err := json.Marshal(meta[mcp.MetaKeyClientCapabilities])
	if err != nil {
		return false
	}
	var capabilities mcp.ClientCapabilities
	return json.Unmarshal(data, &capabilities) == nil && supportsTasks(&capabilities)
}

func missingTasksCapabilityError() error {
	data, _ := json.Marshal(mcp.MissingRequiredClientCapabilityData{
		RequiredCapabilities: &mcp.ClientCapabilities{
			Extensions: map[string]any{tasksExtension: map[string]any{}},
		},
	})
	return &jsonrpc.Error{
		Code: mcp.CodeMissingRequiredClientCapabilities, Message: "tasks capability required but not declared by client", Data: data,
	}
}

func (h *Handler) getTask(ctx context.Context, _ *mcp.ServerSession, params *getTaskParams) (*getTaskResult, error) {
	if params == nil {
		return nil, invalidParams(fmt.Errorf("taskId is required"))
	}
	if !taskParamsSupported(params.GetMeta()) {
		return nil, missingTasksCapabilityError()
	}
	ref, task, err := h.resolveTask(ctx, params.TaskID)
	if err != nil {
		return nil, err
	}
	return detailedTask(params.TaskID, ref, task), nil
}

func (h *Handler) updateTask(ctx context.Context, _ *mcp.ServerSession, params *updateTaskParams) (*completeTaskResult, error) {
	if params == nil {
		return nil, invalidParams(fmt.Errorf("taskId and inputResponses are required"))
	}
	if !taskParamsSupported(params.GetMeta()) {
		return nil, missingTasksCapabilityError()
	}
	ref, task, err := h.resolveTask(ctx, params.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Status.State != a2atype.TaskStateInputRequired {
		return &completeTaskResult{ResultType: "complete"}, nil
	}
	key := inputRequestKey(task)
	inputResponse := params.InputResponses[key]
	if inputResponse == nil {
		return &completeTaskResult{ResultType: "complete"}, nil
	}
	response, ok := inputResponse.(*mcp.ElicitResult)
	if !ok || response == nil {
		return nil, invalidParams(fmt.Errorf("input response %q must be an elicitation result", key))
	}
	message, err := elicitationMessage(task, response)
	if err != nil {
		return nil, invalidParams(err)
	}
	events := h.gateway.SendStreamingMessage(
		context.WithoutCancel(routeContext(ctx, ref.Namespace, ref.InstanceID)),
		&a2atype.SendMessageRequest{Message: message},
	)
	go drain(events)
	return &completeTaskResult{ResultType: "complete"}, nil
}

func (h *Handler) cancelTask(ctx context.Context, _ *mcp.ServerSession, params *cancelTaskParams) (*completeTaskResult, error) {
	if params == nil {
		return nil, invalidParams(fmt.Errorf("taskId is required"))
	}
	if !taskParamsSupported(params.GetMeta()) {
		return nil, missingTasksCapabilityError()
	}
	ref, err := decodeTaskReference(params.TaskID)
	if err != nil {
		return nil, invalidParams(err)
	}
	if _, err := h.gateway.CancelTask(
		routeContext(ctx, ref.Namespace, ref.InstanceID),
		&a2atype.CancelTaskRequest{ID: a2atype.TaskID(ref.TaskID)},
	); err != nil {
		if errors.Is(err, a2atype.ErrTaskNotFound) {
			return nil, invalidParams(err)
		}
		return nil, err
	}
	return &completeTaskResult{ResultType: "complete"}, nil
}

func (h *Handler) resolveTask(ctx context.Context, id string) (taskReference, *a2atype.Task, error) {
	ref, err := decodeTaskReference(id)
	if err != nil {
		return taskReference{}, nil, invalidParams(err)
	}
	task, err := h.gateway.GetTask(
		routeContext(ctx, ref.Namespace, ref.InstanceID),
		&a2atype.GetTaskRequest{ID: a2atype.TaskID(ref.TaskID)},
	)
	if errors.Is(err, a2atype.ErrTaskNotFound) {
		err = invalidParams(err)
	}
	return ref, task, err
}

func invalidParams(err error) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
}

func encodeTaskReference(ref taskReference) (string, error) {
	if err := validateTaskReference(ref); err != nil {
		return "", err
	}
	data, err := json.Marshal(ref)
	if err != nil {
		return "", fmt.Errorf("encode task reference: %w", err)
	}
	return "v1." + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeTaskReference(value string) (taskReference, error) {
	prefix, encoded, ok := strings.Cut(value, ".")
	if !ok || prefix != "v1" {
		return taskReference{}, fmt.Errorf("invalid task ID")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return taskReference{}, fmt.Errorf("invalid task ID: %w", err)
	}
	var ref taskReference
	if err := json.Unmarshal(data, &ref); err != nil {
		return taskReference{}, fmt.Errorf("invalid task ID: %w", err)
	}
	if err := validateTaskReference(ref); err != nil {
		return taskReference{}, fmt.Errorf("invalid task ID: %w", err)
	}
	return ref, nil
}

func validateTaskReference(ref taskReference) error {
	if ref.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if _, err := uuid.Parse(ref.InstanceID); err != nil {
		return fmt.Errorf("invalid AgentInstance ID: %w", err)
	}
	if _, err := uuid.Parse(ref.TaskID); err != nil {
		return fmt.Errorf("invalid A2A task ID: %w", err)
	}
	return nil
}

func taskToMCP(id string, task *a2atype.Task) taskFields {
	created := taskCreatedAt(task)
	updated := created
	if task.Status.Timestamp != nil {
		updated = task.Status.Timestamp.UTC()
	}
	return taskFields{
		TaskID: id, Status: taskStatus(task), StatusMessage: taskText(task),
		CreatedAt: created.Format(time.RFC3339Nano), LastUpdatedAt: updated.Format(time.RFC3339Nano),
		TTLMS: nil, PollIntervalMS: defaultTaskPollInterval,
	}
}

func detailedTask(id string, ref taskReference, task *a2atype.Task) *getTaskResult {
	result := &getTaskResult{taskFields: taskToMCP(id, task), ResultType: "complete"}
	switch task.Status.State {
	case a2atype.TaskStateInputRequired:
		result.InputRequests = inputRequests(task)
	case a2atype.TaskStateCompleted, a2atype.TaskStateFailed, a2atype.TaskStateRejected, a2atype.TaskStateAuthRequired:
		callResult, output := invocationResult(InvokeAgentInstanceInput{Namespace: ref.Namespace, AgentInstanceID: ref.InstanceID}, task)
		callResult.StructuredContent = output
		result.Result = callResult
	}
	return result
}

func taskStatus(task *a2atype.Task) string {
	switch task.Status.State {
	case a2atype.TaskStateSubmitted, a2atype.TaskStateWorking:
		return "working"
	case a2atype.TaskStateInputRequired:
		return "input_required"
	case a2atype.TaskStateCanceled:
		return "cancelled"
	default:
		return "completed"
	}
}

func taskCreatedAt(task *a2atype.Task) time.Time {
	if value, ok := task.Metadata[a2agateway.TaskCreatedAtMetadataKey].(string); ok {
		if created, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return created
		}
	}
	if task.Status.Timestamp != nil {
		return task.Status.Timestamp.UTC()
	}
	return time.Unix(0, 0).UTC()
}

func inputRequests(task *a2atype.Task) mcp.InputRequestMap {
	message := "Agent requires input."
	if task.Status.Message != nil {
		if text := a2aText(task.Status.Message); text != "" {
			message = text
		}
	}
	return mcp.InputRequestMap{inputRequestKey(task): &mcp.ElicitParams{
		Mode: "form", Message: message, RequestedSchema: elicitationSchema(task),
	}}
}

func inputRequestKey(task *a2atype.Task) string {
	if task.Status.Message != nil && task.Status.Message.ID != "" {
		return task.Status.Message.ID
	}
	return string(task.ID)
}

func elicitationText(response *mcp.ElicitResult) (string, error) {
	switch response.Action {
	case "accept":
		text, ok := response.Content["response"].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("accepted elicitation must include a non-empty response")
		}
		return text, nil
	case "decline":
		return "The user declined the request.", nil
	case "cancel":
		return "The user cancelled the request.", nil
	default:
		return "", fmt.Errorf("unsupported elicitation action %q", response.Action)
	}
}

func elicitationSchema(task *a2atype.Task) map[string]any {
	properties := map[string]any{}
	required := []string{}
	if request := adka2a.GetAskUserRequest(task.Status.Message); request != nil {
		for i, question := range request.Questions {
			key := answerKey(i, len(request.Questions))
			property := map[string]any{"type": "string", "description": stringValue(question["question"])}
			if choices := stringSlice(question["choices"]); len(choices) > 0 {
				property["enum"] = choices
			}
			if multiple, _ := question["multiple"].(bool); multiple {
				items := map[string]any{"type": "string"}
				if choices := stringSlice(question["choices"]); len(choices) > 0 {
					items["enum"] = choices
				}
				property = map[string]any{"type": "array", "items": items, "description": stringValue(question["question"])}
			}
			properties[key], required = property, append(required, key)
		}
	} else if request := adka2a.GetToolApprovalRequest(task.Status.Message); request != nil {
		for i, tool := range request.Tools {
			key := fmt.Sprintf("approve_%d", i+1)
			properties[key] = map[string]any{"type": "boolean", "description": fmt.Sprintf("Approve %s", tool.Name)}
			required = append(required, key)
		}
	} else {
		properties["response"], required = map[string]any{"type": "string", "description": "Response to the agent"}, []string{"response"}
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": required, "additionalProperties": false,
	}
}

func elicitationMessage(task *a2atype.Task, response *mcp.ElicitResult) (*a2atype.Message, error) {
	if response == nil || (response.Action != "accept" && response.Action != "decline" && response.Action != "cancel") {
		return nil, fmt.Errorf("unsupported elicitation action")
	}
	message := a2atype.NewMessageForTask(a2atype.MessageRoleUser, task, a2atype.NewTextPart("Tool approval response."))
	if request := adka2a.GetAskUserRequest(task.Status.Message); request != nil {
		answers := make([]adka2a.AskUserAnswer, len(request.Questions))
		var text []string
		for i := range request.Questions {
			if response.Action == "accept" {
				values := stringSlice(response.Content[answerKey(i, len(request.Questions))])
				if len(values) == 0 {
					return nil, fmt.Errorf("accepted elicitation must answer every question")
				}
				answers[i].Answer = values
			} else {
				text, err := elicitationText(response)
				if err != nil {
					return nil, err
				}
				answers[i].Answer = []string{text}
			}
			text = append(text, answers[i].Answer...)
		}
		message.Parts = []*a2atype.Part{a2atype.NewTextPart(strings.Join(text, "\n"))}
		return adka2a.AttachHitlExtension(message, &adka2a.AskUserResponse{
			Type: adka2a.HITLTypeAskUserResponse, ID: request.ID, Answers: answers,
		}), nil
	}
	if request := adka2a.GetToolApprovalRequest(task.Status.Message); request != nil {
		approvals := make([]adka2a.ToolApproval, len(request.Tools))
		for i, tool := range request.Tools {
			approved, ok := response.Content[fmt.Sprintf("approve_%d", i+1)].(bool)
			if response.Action == "accept" && !ok {
				return nil, fmt.Errorf("accepted elicitation must decide every approval")
			}
			approvals[i] = adka2a.ToolApproval{ID: tool.ID, Approved: response.Action == "accept" && approved}
		}
		return adka2a.AttachHitlExtension(message, &adka2a.ToolApprovalResponse{
			Type: adka2a.HITLTypeToolApprovalResponse, Approvals: approvals,
		}), nil
	}
	text, err := elicitationText(response)
	if err != nil {
		return nil, err
	}
	message.Parts = []*a2atype.Part{a2atype.NewTextPart(text)}
	return message, nil
}

func answerKey(i, total int) string {
	if total == 1 {
		return "response"
	}
	return fmt.Sprintf("response_%d", i+1)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringSlice(value any) []string {
	switch value := value.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			return []string{value}
		}
	case []string:
		return value
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

func a2aText(message *a2atype.Message) string {
	var text strings.Builder
	for _, part := range message.Parts {
		if part != nil {
			text.WriteString(part.Text())
		}
	}
	return text.String()
}
