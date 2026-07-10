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
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// userIDContextKey is the context key for passing the session user_id to the subagent.
type userIDContextKey struct{}

// parentContextIDContextKey is the context key carrying this agent's own
// A2A context_id (== ADK session id) into the outbound interceptor so it can
// be stamped as the parent_context_id header on every outbound A2A call.
type parentContextIDContextKey struct{}

// Conversation-lineage headers stamped on outbound A2A calls so a remote
// agent can correlate this turn with the originating chat conversation -
// useful when downstream code keys per-conversation state (sessions, sandbox
// pods, cache entries) on a stable identifier across A2A hops.
//
// ParentContextIDHeader is the immediate caller's A2A context_id (the
// session id of the agent that ran this tool). It changes with every hop in
// a chain of A2A calls.
//
// RootContextIDHeader is the top-of-chain context_id - the agent at the
// start of the chain (typically the user-facing chat agent). It stays
// stable across every hop and across every turn of the same conversation,
// so downstream agents can key state that should outlive a single A2A call
// (e.g. claim a per-conversation worker pod that survives between turns).
//
// Mirrors the Python ADK constants in
// python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py.
const (
	ParentContextIDHeader = "x-kagent-parent-context-id"
	RootContextIDHeader   = "x-kagent-root-context-id"
)

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

// lineageHeadersInterceptor stamps the parent + root context_id headers on
// every outbound A2A call. Parent comes from a context value populated by the
// caller (the tool's own ADK session id). Root is forwarded unchanged from the
// inbound A2A request when present (so the value set by the agent at the start
// of the chain survives every hop), with a fallback to our own session id when
// this agent is the chain root.
//
// Pre-existing headers on req.Meta win (analogous to Python's header_provider
// override), so a caller that sets extraHeaders for either header keeps full
// control.
type lineageHeadersInterceptor struct {
	a2aclient.PassthroughInterceptor
}

func (l *lineageHeadersInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	parent, _ := ctx.Value(parentContextIDContextKey{}).(string)
	if parent == "" {
		return ctx, nil
	}

	var inboundRoot string
	if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
		if meta := callCtx.RequestMeta(); meta != nil {
			if vals, ok := meta.Get(RootContextIDHeader); ok && len(vals) > 0 {
				inboundRoot = vals[0]
			}
		}
	}

	root := inboundRoot
	if root == "" {
		root = parent
	}

	if len(req.Meta.Get(ParentContextIDHeader)) == 0 {
		req.Meta.Append(ParentContextIDHeader, parent)
	}
	if len(req.Meta.Get(RootContextIDHeader)) == 0 {
		req.Meta.Append(RootContextIDHeader, root)
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

	// sharedContextID is the stable A2A context_id used for every call to this
	// sub-agent when isolateSessions is false (the default): all calls land
	// in one shared sub-agent session, giving stateful sub-agents session
	// continuity across calls. Unused when isolateSessions is true — each
	// call mints its own id instead (see contextIDForCall).
	sharedContextID string

	// isolateSessions mints a fresh context_id per call (see contextIDForCall)
	// instead of reusing sharedContextID, so each call runs in its own isolated
	// sub-agent session. Required for parallel fan-out: without it, N
	// parallel calls in one turn collapse into a single shared sub-agent
	// session. See go/api/v1alpha2.Tool.IsolateSessions.
	isolateSessions bool
}

// remoteA2AResponse is the typed return value for every remote A2A tool
// invocation. Using one shared struct (instead of ad-hoc map[string]any
// literals per branch) means every response path — success, input_required,
// and failure — carries the same fields, so a field like SubagentSessionID
// can't be silently forgotten in one branch while present in another.
// functiontool.New infers the tool's output schema from this type.
type remoteA2AResponse struct {
	Result              string         `json:"result,omitempty"`
	Error               string         `json:"error,omitempty"`
	Status              string         `json:"status,omitempty"`
	WaitingFor          string         `json:"waiting_for,omitempty"`
	Subagent            string         `json:"subagent,omitempty"`
	SubagentSessionID   string         `json:"subagent_session_id,omitempty"`
	KAgentUsageMetadata map[string]any `json:"kagent_usage_metadata,omitempty"`
}

// NewKAgentRemoteA2ATool creates a function tool that calls a remote A2A agent
// and propagates HITL state.
//
// It intentionally does not return a session id: the constructor-time
// context_id used to be pre-stamped onto outbound function_call events so
// the UI could link the Activity panel before a response arrived, but that
// model only works when a tool has exactly one session for its whole
// lifetime — it breaks down for isolateSessions, where the real session is
// per invocation, not per tool instance. Every call now reports its own
// actual context_id back as SubagentSessionID in the tool's response (see
// remoteA2AResponse), which is the single source of truth the UI reads from
// for both isolated and non-isolated tools alike.
//
// The agent card is fetched lazily from baseURL/.well-known/agent.json.
// If httpClient is nil, a default client is created. The client's transport is
// wrapped with otelhttp to propagate W3C trace context to subagents.
func NewKAgentRemoteA2ATool(name, description, baseURL string, httpClient *http.Client, extraHeaders map[string]string, propagateToken, isolateSessions bool) (tool.Tool, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	httpClient = withOTelTransport(httpClient)
	state := &remoteA2AState{
		name:            name,
		description:     description,
		baseURL:         baseURL,
		httpClient:      httpClient,
		extraHeaders:    extraHeaders,
		propagateToken:  propagateToken,
		sharedContextID: a2atype.NewContextID(),
		isolateSessions: isolateSessions,
	}
	ft, err := functiontool.New(functiontool.Config{
		Name:        name,
		Description: description,
	}, func(ctx adkagent.ToolContext, in remoteA2AInput) (remoteA2AResponse, error) {
		return state.run(ctx, in.Request)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote A2A function tool for %s: %w", name, err)
	}
	return ft, nil
}

// contextIDForCall returns the A2A context_id to stamp on the next outbound
// call: a fresh id when isolateSessions is enabled (isolated per-call
// session), or the tool's stable sharedContextID otherwise (shared session
// for the lifetime of the tool).
func (s *remoteA2AState) contextIDForCall() string {
	if s.isolateSessions {
		return a2atype.NewContextID()
	}
	return s.sharedContextID
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
			&lineageHeadersInterceptor{},
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
func (s *remoteA2AState) run(ctx adkagent.ToolContext, requestText string) (remoteA2AResponse, error) {
	if ctx.ToolConfirmation() != nil {
		return s.handleResume(ctx)
	}
	return s.handleFirstCall(ctx, requestText)
}

// handleFirstCall is Phase 1: send the request to the remote agent.
func (s *remoteA2AState) handleFirstCall(ctx adkagent.ToolContext, requestText string) (remoteA2AResponse, error) {
	if requestText == "" {
		return remoteA2AResponse{Error: "missing or empty 'request' argument"}, nil
	}

	client, err := s.ensureClient(ctx)
	if err != nil {
		return remoteA2AResponse{Error: err.Error()}, nil
	}

	contextID := s.contextIDForCall()
	message := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		a2atype.TextPart{Text: requestText},
	)
	message.ContextID = contextID

	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	sendCtx = context.WithValue(sendCtx, parentContextIDContextKey{}, ctx.SessionID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent request failed", "tool", s.name, "error", err)
		return remoteA2AResponse{Error: fmt.Sprintf("Remote agent '%s' request failed: %v", s.name, err)}, nil
	}

	return s.processResult(ctx, contextID, result)
}

// handleResume is Phase 2: forward the user's decision to the remote agent's pending task.
func (s *remoteA2AState) handleResume(ctx adkagent.ToolContext) (remoteA2AResponse, error) {
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
		return remoteA2AResponse{Error: fmt.Sprintf("Cannot resume remote agent '%s': missing task context.", subagentName)}, nil
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
		return remoteA2AResponse{Error: err.Error()}, nil
	}

	sendCtx := context.WithValue(ctx, userIDContextKey{}, ctx.UserID())
	sendCtx = context.WithValue(sendCtx, parentContextIDContextKey{}, ctx.SessionID())
	result, err := client.SendMessage(sendCtx, &a2atype.MessageSendParams{Message: message})
	if err != nil {
		slog.Error("Remote agent resume failed", "tool", subagentName, "error", err)
		return remoteA2AResponse{Error: fmt.Sprintf("Remote agent '%s' resume failed: %v", subagentName, err)}, nil
	}

	// contextID here is whatever the confirmation payload carried (the
	// original subagent session from the paused task). It is always the
	// correct id to report — unlike before, there is no fallback to a
	// construction-time id: for an isolated tool, sharedContextID was never
	// actually used in any A2A call, so falling back to it would report a
	// session id that doesn't correspond to any real subagent activity.
	return s.processResult(ctx, contextID, result)
}

// processResult converts a SendMessageResult into a tool return value.
// contextID is the A2A context_id this call was sent under (from
// contextIDForCall, or the confirmation payload on resume) and is reported
// back as SubagentSessionID on every branch — success, input_required, and
// failure alike — so the UI's AgentCallDisplay can always link the card to
// the session that actually ran the call. This is the single source of
// truth the UI reads from; there is no separate constructor-time id to fall
// back on (see NewKAgentRemoteA2ATool).
func (s *remoteA2AState) processResult(ctx adkagent.ToolContext, contextID string, result a2atype.SendMessageResult) (remoteA2AResponse, error) {
	switch r := result.(type) {
	case *a2atype.Message:
		return remoteA2AResponse{
			Result:            extractTextFromMessage(r),
			SubagentSessionID: contextID,
		}, nil
	case *a2atype.Task:
		switch r.Status.State {
		case a2atype.TaskStateInputRequired:
			return s.handleInputRequired(ctx, r, contextID), nil
		case a2atype.TaskStateFailed:
			text := extractTextFromTask(r)
			if text == "" {
				text = fmt.Sprintf("Remote agent '%s' failed.", s.name)
			}
			return remoteA2AResponse{
				Error:             text,
				SubagentSessionID: contextID,
			}, nil
		default:
			// completed — include sub-agent's final LLM usage from task.metadata
			// so the parent can display it on the AgentCall card in the UI.
			// Mirrors Python's _extract_usage_from_task(task).
			ret := remoteA2AResponse{
				Result:            extractTextFromTask(r),
				SubagentSessionID: contextID,
			}
			if usage := extractUsageFromTask(r); usage != nil {
				ret.KAgentUsageMetadata = usage
			}
			return ret, nil
		}
	default:
		return remoteA2AResponse{
			Error:             fmt.Sprintf("Remote agent '%s' returned no result.", s.name),
			SubagentSessionID: contextID,
		}, nil
	}
}

// handleInputRequired pauses parent agent execution via RequestConfirmation.
// contextID is reported back as SubagentSessionID so the UI can link the
// pending Activity panel to the paused subagent session before the human
// decision is forwarded and a final result comes back.
func (s *remoteA2AState) handleInputRequired(ctx adkagent.ToolContext, task *a2atype.Task, contextID string) remoteA2AResponse {
	if task == nil {
		slog.Error("Subagent returned input_required without task", "tool", s.name)
		return remoteA2AResponse{
			Error:             fmt.Sprintf("Remote agent '%s' returned input_required without task context.", s.name),
			SubagentSessionID: contextID,
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
	return remoteA2AResponse{
		Status:            "pending",
		WaitingFor:        "subagent_approval",
		Subagent:          s.name,
		SubagentSessionID: contextID,
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
