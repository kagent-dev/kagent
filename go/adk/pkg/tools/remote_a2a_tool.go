package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// SubagentSessionProvider is a tool that delegates to a remote subagent and
// can expose the subagent's session ID for live activity polling.
type SubagentSessionProvider interface {
	tool.Tool
	SubagentSessionID() string
}

// userIDContextKey is the context key for passing the session user_id to the subagent.
type userIDContextKey struct{}

// User ID forwarding interceptor
type userIDForwardingInterceptor struct {
	a2aclient.PassthroughInterceptor
}

func (u *userIDForwardingInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	if uid, ok := ctx.Value(userIDContextKey{}).(string); ok && uid != "" {
		req.Meta.Append("x-user-id", uid)
	}
	return ctx, nil
}

// KAgentRemoteA2ATool calls a remote A2A agent and propagates HITL state.
type KAgentRemoteA2ATool struct {
	name         string
	description  string
	baseURL      string
	httpClient   *http.Client
	extraHeaders map[string]string

	a2aClient *a2aclient.Client
	agentCard *a2atype.AgentCard

	lastContextID string
}

// NewKAgentRemoteA2ATool creates a KAgentRemoteA2ATool.
// baseURL is the base URL of the remote agent (e.g. http://host:port).
// The agent card is fetched lazily from baseURL/.well-known/agent.json.
// If httpClient is nil, http.DefaultClient is used.
func NewKAgentRemoteA2ATool(name, description, baseURL string, httpClient *http.Client, extraHeaders map[string]string) *KAgentRemoteA2ATool {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &KAgentRemoteA2ATool{
		name:          name,
		description:   description,
		baseURL:       baseURL,
		httpClient:    httpClient,
		extraHeaders:  extraHeaders,
		lastContextID: a2atype.NewContextID(),
	}
}

// Name implements tool.Tool.
func (t *KAgentRemoteA2ATool) Name() string { return t.name }

// Description implements tool.Tool.
func (t *KAgentRemoteA2ATool) Description() string { return t.description }

// IsLongRunning implements tool.Tool.
func (t *KAgentRemoteA2ATool) IsLongRunning() bool { return false }

// SubagentSessionID implements SubagentSessionProvider.
func (t *KAgentRemoteA2ATool) SubagentSessionID() string { return t.lastContextID }

// Declaration returns the GenAI FunctionDeclaration. Mirrors AgentTool schema.
func (t *KAgentRemoteA2ATool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.name,
		Description: t.description,
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"request": {
					Type:        genai.TypeString,
					Description: "The question or task to send to this subagent.",
				},
			},
			Required: []string{"request"},
		},
	}
}

// Run executes the remote agent tool.
func (t *KAgentRemoteA2ATool) Run(ctx tool.Context, args any) (map[string]any, error) {
	if ctx.ToolConfirmation() != nil {
		return t.handleResume(ctx)
	}
	return t.handleFirstCall(ctx, args)
}

// ProcessRequest packs the tool's FunctionDeclaration into the LLM request
// so the model knows it can call this remote A2A agent tool.
func (t *KAgentRemoteA2ATool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	return packTool(req, t.Name(), t.Declaration(), t)
}

// ensureClient lazily resolves the agent card and initialises the A2A client.
func (t *KAgentRemoteA2ATool) ensureClient(ctx context.Context) (*a2aclient.Client, error) {
	if t.a2aClient != nil {
		return t.a2aClient, nil
	}

	resolver := agentcard.NewResolver(t.httpClient)

	// Build ResolveOptions for extra headers.
	var resolveOpts []agentcard.ResolveOption
	for k, v := range t.extraHeaders {
		resolveOpts = append(resolveOpts, agentcard.WithRequestHeader(k, v))
	}

	card, err := resolver.Resolve(ctx, t.baseURL, resolveOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent card for %s: %w", t.name, err)
	}
	t.agentCard = card

	// Auto-populate description from agent card when not explicitly set.
	if t.description == "" && card.Description != "" {
		t.description = card.Description
	}

	opts := []a2aclient.FactoryOption{
		a2aclient.WithJSONRPCTransport(t.httpClient),
	}
	// Always inject x-kagent-source: subagent to mark this as a subagent call.
	meta := a2aclient.CallMeta{}
	meta.Append("x-kagent-source", "agent")
	for k, v := range t.extraHeaders {
		meta.Append(k, v)
	}
	opts = append(opts, a2aclient.WithInterceptors(
		a2aclient.NewStaticCallMetaInjector(meta),
		&userIDForwardingInterceptor{},
	))

	client, err := a2aclient.NewFromCard(ctx, card, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create A2A client for %s: %w", t.name, err)
	}
	t.a2aClient = client
	return client, nil
}

// handleFirstCall is Phase 1: send the request to the remote agent.
func (t *KAgentRemoteA2ATool) handleFirstCall(ctx tool.Context, args any) (map[string]any, error) {
	argsMap, _ := args.(map[string]any)
	requestText, _ := argsMap["request"].(string)

	client, err := t.ensureClient(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	message := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		a2atype.TextPart{Text: requestText},
	)
	message.ContextID = t.lastContextID

	// Propagate the session user_id to the subagent via x-user-id header.
	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent request failed", "tool", t.name, "error", err)
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' request failed: %v", t.name, err)}, nil
	}

	return t.processResult(ctx, result)
}

// handleResume is Phase 2: forward the user's decision to the remote agent's pending task.
func (t *KAgentRemoteA2ATool) handleResume(ctx tool.Context) (map[string]any, error) {
	confirmation := ctx.ToolConfirmation()
	payload, _ := confirmation.Payload.(map[string]any)
	hitlPayload := a2a.ParseHitlConfirmationPayload(payload)

	taskID := hitlPayload.TaskID
	contextID := hitlPayload.ContextID
	subagentName := hitlPayload.SubagentName
	if subagentName == "" {
		subagentName = t.name
	}

	if taskID == "" {
		slog.Error("Resume for remote agent but no task_id in confirmation payload", "tool", t.name)
		return map[string]any{"error": fmt.Sprintf("Cannot resume remote agent '%s': missing task context.", subagentName)}, nil
	}

	// Build the decision DataPart to forward to the subagent.
	decisionData := buildDecisionData(confirmation.Confirmed, hitlPayload)

	message := &a2atype.Message{
		ID:        a2atype.NewMessageID(),
		TaskID:    a2atype.TaskID(taskID),
		ContextID: contextID,
		Role:      a2atype.MessageRoleUser,
		Parts:     a2atype.ContentParts{a2atype.DataPart{Data: decisionData}},
	}

	decisionType, _ := decisionData[a2a.KAgentHitlDecisionTypeKey].(string)
	slog.Info("Forwarding decision to subagent",
		"decisionType", decisionType,
		"subagent", subagentName,
		"taskID", taskID,
	)

	client, err := t.ensureClient(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	// Propagate the session user_id to the subagent via x-user-id header.
	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent resume failed", "tool", subagentName, "error", err)
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' resume failed: %v", subagentName, err)}, nil
	}

	ret, retErr := t.processResult(ctx, result)
	// Prefer the context_id from the confirmation payload (the original subagent
	// session) over the pre-generated one. Mirrors Python's:
	//   "subagent_session_id": context_id or self._last_context_id
	if retErr == nil && ret != nil {
		sessionID := contextID
		if sessionID == "" {
			sessionID = t.lastContextID
		}
		ret["subagent_session_id"] = sessionID
	}
	return ret, retErr
}

// processResult converts a SendMessageResult into a tool return value,
// handling completed, input_required, and failed states.
func (t *KAgentRemoteA2ATool) processResult(ctx tool.Context, result a2atype.SendMessageResult) (map[string]any, error) {
	switch r := result.(type) {
	case *a2atype.Message:
		return map[string]any{"result": extractTextFromMessage(r)}, nil
	case *a2atype.Task:
		state := r.Status.State
		switch state {
		case a2atype.TaskStateInputRequired:
			return t.handleInputRequired(ctx, r), nil
		case a2atype.TaskStateFailed:
			text := extractTextFromTask(r)
			if text == "" {
				text = fmt.Sprintf("Remote agent '%s' failed.", t.name)
			}
			return map[string]any{"error": text}, nil
		default:
			// completed — include sub-agent's final LLM usage from task.metadata
			// so the parent can display it on the AgentCall card in the UI.
			// Mirrors Python's _extract_usage_from_task(task).
			text := extractTextFromTask(r)
			result := map[string]any{
				"result":              text,
				"subagent_session_id": t.lastContextID,
			}
			if usage := extractUsageFromTask(r); usage != nil {
				result["kagent_usage_metadata"] = usage
			}
			return result, nil
		}
	default:
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' returned no result.", t.name)}, nil
	}
}

// handleInputRequired pauses parent agent execution via RequestConfirmation,
// storing task_id and context_id so the resume path can forward the decision.
func (t *KAgentRemoteA2ATool) handleInputRequired(ctx tool.Context, task *a2atype.Task) map[string]any {
	if task == nil {
		slog.Error("Subagent returned input_required without task", "tool", t.name)
		return map[string]any{
			"error": fmt.Sprintf("Remote agent '%s' returned input_required without task context.", t.name),
		}
	}

	var hitlParts []a2a.HitlPartInfo
	if task.Status.Message != nil {
		hitlParts = a2a.ExtractHitlInfoFromParts(task.Status.Message.Parts)
	}

	var innerToolNames []string
	for _, hp := range hitlParts {
		if hp.OriginalFunctionCall.Name != "" {
			innerToolNames = append(innerToolNames, hp.OriginalFunctionCall.Name)
		}
	}

	var hint string
	if len(innerToolNames) > 0 {
		hint = fmt.Sprintf("Remote agent '%s' requires approval for tool(s): %s",
			t.name, strings.Join(innerToolNames, ", "))
	} else {
		hint = fmt.Sprintf("Remote agent '%s' requires human input before continuing.", t.name)
	}

	confirmPayload := a2a.HitlConfirmationPayload{
		TaskID:       string(task.ID),
		ContextID:    task.ContextID,
		SubagentName: t.name,
		HitlParts:    hitlParts,
	}

	slog.Info("Subagent returned input_required, requesting confirmation from parent",
		"tool", t.name, "taskID", task.ID)

	if err := ctx.RequestConfirmation(hint, confirmPayload.ToMap()); err != nil {
		slog.Error("Failed to request confirmation", "tool", t.name, "error", err)
	}
	return map[string]any{
		"status":      "pending",
		"waiting_for": "subagent_approval",
		"subagent":    t.name,
	}
}

// buildDecisionData constructs the decision DataPart.Data map to forward to the subagent.
func buildDecisionData(confirmed bool, payload a2a.HitlConfirmationPayload) map[string]any {
	switch {
	case len(payload.BatchDecisions) > 0:
		batchDecisions := make(map[string]any, len(payload.BatchDecisions))
		for id, decision := range payload.BatchDecisions {
			batchDecisions[id] = string(decision)
		}
		data := map[string]any{
			a2a.KAgentHitlDecisionTypeKey: a2a.KAgentHitlDecisionTypeBatch,
			a2a.KAgentHitlDecisionsKey:    batchDecisions,
		}
		if len(payload.RejectionReasons) > 0 {
			rejReasons := make(map[string]any, len(payload.RejectionReasons))
			for id, reason := range payload.RejectionReasons {
				rejReasons[id] = reason
			}
			data[a2a.KAgentHitlRejectionReasonsKey] = rejReasons
		}
		return data

	case len(payload.Answers) > 0:
		askUserAnswers := make([]map[string]any, 0, len(payload.Answers))
		for _, answer := range payload.Answers {
			askUserAnswers = append(askUserAnswers, map[string]any{"answer": answer.Answer})
		}
		// Forward as approve + answers so the subagent's HITL processor takes the ask_user path.
		return map[string]any{
			a2a.KAgentHitlDecisionTypeKey: a2a.KAgentHitlDecisionTypeApprove,
			a2a.KAgentAskUserAnswersKey:   askUserAnswers,
		}

	default:
		decisionType := a2a.KAgentHitlDecisionTypeApprove
		if !confirmed {
			decisionType = a2a.KAgentHitlDecisionTypeReject
		}
		data := map[string]any{a2a.KAgentHitlDecisionTypeKey: decisionType}
		if !confirmed {
			if payload.RejectionReason != "" {
				data["rejection_reason"] = payload.RejectionReason
			}
		}
		return data
	}
}

// extractUsageFromTask extracts kagent_usage_metadata from a completed task.
// The A2A task manager merges the final TaskStatusUpdateEvent.metadata into
// task.metadata, so LLM usage is available here for non-streaming callers.
// Port of _remote_a2a_tool.py:_extract_usage_from_task().
func extractUsageFromTask(task *a2atype.Task) map[string]any {
	if task == nil || task.Metadata == nil {
		return nil
	}
	usage, ok := task.Metadata["kagent_usage_metadata"].(map[string]any)
	if ok && len(usage) > 0 {
		return usage
	}
	return nil
}

// extractTextFromTask extracts the text result from a completed Task.
func extractTextFromTask(task *a2atype.Task) string {
	if task == nil {
		return ""
	}
	// Prefer artifacts (canonical result).
	if len(task.Artifacts) > 0 {
		var texts []string
		for _, artifact := range task.Artifacts {
			for _, part := range artifact.Parts {
				if tp, ok := part.(a2atype.TextPart); ok && tp.Text != "" {
					texts = append(texts, tp.Text)
				}
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	// Fall back to status message.
	if task.Status.Message != nil {
		return extractTextFromMessage(task.Status.Message)
	}
	return ""
}

// extractTextFromMessage extracts text from a direct A2A Message response.
func extractTextFromMessage(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	var texts []string
	for _, part := range message.Parts {
		if tp, ok := part.(a2atype.TextPart); ok && tp.Text != "" {
			texts = append(texts, tp.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// KAgentRemoteA2AToolset wraps KAgentRemoteA2ATool as an ADK Toolset so the
// runner's close path can destroy the underlying A2A client.
type KAgentRemoteA2AToolset struct {
	tool *KAgentRemoteA2ATool
}

// NewKAgentRemoteA2AToolset creates a KAgentRemoteA2AToolset.
func NewKAgentRemoteA2AToolset(name, description, baseURL string, httpClient *http.Client, extraHeaders map[string]string) *KAgentRemoteA2AToolset {
	return &KAgentRemoteA2AToolset{
		tool: NewKAgentRemoteA2ATool(name, description, baseURL, httpClient, extraHeaders),
	}
}

// Name returns the toolset name (== inner tool name).
func (s *KAgentRemoteA2AToolset) Name() string { return s.tool.name }

// SubagentSessionID implements SubagentSessionProvider at the toolset level.
func (s *KAgentRemoteA2AToolset) SubagentSessionID() string { return s.tool.SubagentSessionID() }

// Tools implements tool.Toolset.
func (s *KAgentRemoteA2AToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	return []tool.Tool{s.tool}, nil
}

// Close destroys the underlying A2A client, releasing any connections.
func (s *KAgentRemoteA2AToolset) Close() error {
	if s.tool.a2aClient != nil {
		if err := s.tool.a2aClient.Destroy(); err != nil {
			slog.Warn("Failed to destroy A2A client", "tool", s.tool.name, "error", err)
		}
		s.tool.a2aClient = nil
	}
	return nil
}
