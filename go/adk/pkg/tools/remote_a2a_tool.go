package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// userIDContextKey is the context key for passing the session user_id to the subagent.
type userIDContextKey struct{}

// userIDForwardingInterceptor forwards the session user_id as an x-user-id header.
type userIDForwardingInterceptor struct {
	a2aclient.PassthroughInterceptor
}

func (u *userIDForwardingInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	if uid, ok := ctx.Value(userIDContextKey{}).(string); ok && uid != "" {
		req.Meta.Append("x-user-id", uid)
	}
	return ctx, nil
}

// authzForwardingInterceptor forwards the Authorization header from the
// incoming A2A request context to outbound sub-agent A2A calls.
type authzForwardingInterceptor struct {
	a2aclient.PassthroughInterceptor
}

func (a *authzForwardingInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return ctx, nil
	}
	meta := callCtx.RequestMeta()
	if meta == nil {
		return ctx, nil
	}
	if len(req.Meta.Get(constants.AuthorizationHeader)) > 0 {
		return ctx, nil
	}
	if vals, ok := meta.Get(constants.AuthorizationHeader); ok && len(vals) > 0 && vals[0] != "" {
		req.Meta.Append(constants.AuthorizationHeader, vals[0])
	}
	return ctx, nil
}

// remoteA2AInput is the typed argument for the remote A2A function tool.
type remoteA2AInput struct {
	Request string `json:"request"`
}

// remoteA2AState holds the mutable state for one remote A2A agent connection.
// All external interaction goes through the tool.Tool returned by NewKAgentRemoteA2ATool.
type remoteA2AState struct {
	name           string
	description    string
	baseURL        string
	httpClient     *http.Client
	extraHeaders   map[string]string
	propagateToken bool

	a2aClient *a2aclient.Client
	agentCard *a2atype.AgentCard
	initOnce  sync.Once
	initErr   error

	lastContextID string
}

// NewKAgentRemoteA2ATool creates a function tool that calls a remote A2A agent and
// propagates HITL state. It returns:
//   - the tool.Tool to register with the agent config
//   - the initial A2A context/session ID for subagent session stamping
//
// The agent card is fetched lazily from baseURL/.well-known/agent.json.
// If httpClient is nil, a default client is created. The client's transport is
// wrapped with otelhttp to propagate W3C trace context to subagents.
func NewKAgentRemoteA2ATool(name, description, baseURL string, httpClient *http.Client, extraHeaders map[string]string, propagateToken bool) (tool.Tool, string, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	httpClient = withOTelTransport(httpClient)
	state := &remoteA2AState{
		name:           name,
		description:    description,
		baseURL:        baseURL,
		httpClient:     httpClient,
		extraHeaders:   extraHeaders,
		propagateToken: propagateToken,
		lastContextID:  a2atype.NewContextID(),
	}
	ft, err := functiontool.New(functiontool.Config{
		Name:        name,
		Description: description,
	}, func(ctx tool.Context, in remoteA2AInput) (map[string]any, error) {
		return state.run(ctx, in.Request)
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create remote A2A function tool for %s: %w", name, err)
	}
	return ft, state.lastContextID, nil
}

// ensureClient lazily resolves the agent card and initialises the A2A client.
// Initialization is protected by sync.Once to avoid races under concurrent use.
func (s *remoteA2AState) ensureClient(ctx context.Context) (*a2aclient.Client, error) {
	s.initOnce.Do(func() {
		resolver := agentcard.NewResolver(s.httpClient)

		var resolveOpts []agentcard.ResolveOption
		for k, v := range s.extraHeaders {
			resolveOpts = append(resolveOpts, agentcard.WithRequestHeader(k, v))
		}

		card, err := resolver.Resolve(ctx, s.baseURL, resolveOpts...)
		if err != nil {
			s.initErr = fmt.Errorf("failed to resolve agent card for %s: %w", s.name, err)
			return
		}
		s.agentCard = card

		// Auto-populate description from agent card when not explicitly set.
		if s.description == "" && card.Description != "" {
			s.description = card.Description
		}

		opts := []a2aclient.FactoryOption{
			a2aclient.WithJSONRPCTransport(s.httpClient),
		}
		// Always inject x-kagent-source: agent to mark this as an agent-originated call.
		meta := a2aclient.CallMeta{}
		meta.Append("x-kagent-source", "agent")
		for k, v := range s.extraHeaders {
			meta.Append(k, v)
		}
		interceptors := []a2aclient.CallInterceptor{
			a2aclient.NewStaticCallMetaInjector(meta),
			&userIDForwardingInterceptor{},
		}
		if s.propagateToken {
			interceptors = append(interceptors, &authzForwardingInterceptor{})
		}
		opts = append(opts, a2aclient.WithInterceptors(interceptors...))

		client, err := a2aclient.NewFromCard(ctx, card, opts...)
		if err != nil {
			s.initErr = fmt.Errorf("failed to create A2A client for %s: %w", s.name, err)
			return
		}
		s.a2aClient = client
	})
	return s.a2aClient, s.initErr
}

// run dispatches to handleResume or handleFirstCall based on ToolConfirmation presence.
func (s *remoteA2AState) run(ctx tool.Context, requestText string) (map[string]any, error) {
	if ctx.ToolConfirmation() != nil {
		return s.handleResume(ctx)
	}
	return s.handleFirstCall(ctx, requestText)
}

// handleFirstCall is Phase 1: send the request to the remote agent.
func (s *remoteA2AState) handleFirstCall(ctx tool.Context, requestText string) (map[string]any, error) {
	if requestText == "" {
		return map[string]any{"error": "missing or empty 'request' argument"}, nil
	}

	client, err := s.ensureClient(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	message := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		a2atype.TextPart{Text: requestText},
	)
	message.ContextID = s.lastContextID

	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent request failed", "tool", s.name, "error", err)
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' request failed: %v", s.name, err)}, nil
	}

	return s.processResult(ctx, result)
}

// handleResume is Phase 2: forward the user's decision to the remote agent's pending task.
func (s *remoteA2AState) handleResume(ctx tool.Context) (map[string]any, error) {
	confirmation := ctx.ToolConfirmation()
	payload, _ := confirmation.Payload.(map[string]any)
	hitlPayload := a2a.ParseHitlConfirmationPayload(payload)

	taskID := hitlPayload.TaskID
	contextID := hitlPayload.ContextID
	subagentName := hitlPayload.SubagentName
	if subagentName == "" {
		subagentName = s.name
	}

	if taskID == "" {
		slog.Error("Resume for remote agent but no task_id in confirmation payload", "tool", s.name)
		return map[string]any{"error": fmt.Sprintf("Cannot resume remote agent '%s': missing task context.", subagentName)}, nil
	}

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

	client, err := s.ensureClient(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent resume failed", "tool", subagentName, "error", err)
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' resume failed: %v", subagentName, err)}, nil
	}

	ret, retErr := s.processResult(ctx, result)
	// Prefer the context_id from the confirmation payload (the original subagent
	// session) over the pre-generated one. Mirrors Python's:
	//   "subagent_session_id": context_id or self._last_context_id
	if retErr == nil && ret != nil {
		sessionID := contextID
		if sessionID == "" {
			sessionID = s.lastContextID
		}
		ret["subagent_session_id"] = sessionID
	}
	return ret, retErr
}

// processResult converts a SendMessageResult into a tool return value.
func (s *remoteA2AState) processResult(ctx tool.Context, result a2atype.SendMessageResult) (map[string]any, error) {
	switch r := result.(type) {
	case *a2atype.Message:
		return map[string]any{"result": extractTextFromMessage(r)}, nil
	case *a2atype.Task:
		switch r.Status.State {
		case a2atype.TaskStateInputRequired:
			return s.handleInputRequired(ctx, r), nil
		case a2atype.TaskStateFailed:
			text := extractTextFromTask(r)
			if text == "" {
				text = fmt.Sprintf("Remote agent '%s' failed.", s.name)
			}
			return map[string]any{"error": text}, nil
		default:
			// completed — include sub-agent's final LLM usage from task.metadata
			// so the parent can display it on the AgentCall card in the UI.
			// Mirrors Python's _extract_usage_from_task(task).
			text := extractTextFromTask(r)
			ret := map[string]any{
				"result":              text,
				"subagent_session_id": s.lastContextID,
			}
			if usage := extractUsageFromTask(r); usage != nil {
				ret["kagent_usage_metadata"] = usage
			}
			return ret, nil
		}
	default:
		return map[string]any{"error": fmt.Sprintf("Remote agent '%s' returned no result.", s.name)}, nil
	}
}

// handleInputRequired pauses parent agent execution via RequestConfirmation.
func (s *remoteA2AState) handleInputRequired(ctx tool.Context, task *a2atype.Task) map[string]any {
	if task == nil {
		slog.Error("Subagent returned input_required without task", "tool", s.name)
		return map[string]any{
			"error": fmt.Sprintf("Remote agent '%s' returned input_required without task context.", s.name),
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
			s.name, strings.Join(innerToolNames, ", "))
	} else {
		hint = fmt.Sprintf("Remote agent '%s' requires human input before continuing.", s.name)
	}

	confirmPayload := a2a.HitlConfirmationPayload{
		TaskID:       string(task.ID),
		ContextID:    task.ContextID,
		SubagentName: s.name,
		HitlParts:    hitlParts,
	}

	slog.Info("Subagent returned input_required, requesting confirmation from parent",
		"tool", s.name, "taskID", task.ID)

	if err := ctx.RequestConfirmation(hint, confirmPayload.ToMap()); err != nil {
		slog.Error("Failed to request confirmation", "tool", s.name, "error", err)
	}
	return map[string]any{
		"status":      "pending",
		"waiting_for": "subagent_approval",
		"subagent":    s.name,
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
		if !confirmed && payload.RejectionReason != "" {
			data["rejection_reason"] = payload.RejectionReason
		}
		return data
	}
}

// withOTelTransport returns a shallow copy of the client whose transport is
// wrapped with otelhttp. This injects W3C traceparent/tracestate headers on
// outbound requests so subagent spans are linked to the parent trace.
func withOTelTransport(c *http.Client) *http.Client {
	cp := *c
	transport := cp.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cp.Transport = otelhttp.NewTransport(transport)
	return &cp
}

// extractUsageFromTask extracts kagent_usage_metadata from a completed task.
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
