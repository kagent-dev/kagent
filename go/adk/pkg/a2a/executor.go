package a2a

import (
	"context"
	"fmt"
	"os"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/skills"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	defaultSkillsDirectory = "/skills"
	envSkillsFolder        = "KAGENT_SKILLS_FOLDER"
	sessionNameMaxLength   = 20
)

// NewExecutorConfig builds an adka2a.ExecutorConfig with kagent-specific
// callbacks wired in. The returned config can be passed directly to
// adka2a.NewExecutor.
//
// subagentSessionIDs maps tool name → pre-generated context_id for each
// KAgentRemoteA2ATool. When non-empty the genAIPartConverter stamps
// kagent_subagent_session_id onto outbound function_call DataParts so the UI
// can start polling the subagent session while the tool is running.
// Mirrors Python's executor which builds this map from SubagentSessionProvider
// tools before each invocation.
func NewExecutorConfig(
	runnerConfig runner.Config,
	sessionService *session.KAgentSessionService,
	subagentSessionIDs map[string]string,
	stream bool,
	appName string,
	logger logr.Logger,
) adka2a.ExecutorConfig {
	skillsDir := os.Getenv(envSkillsFolder)
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}

	var runConfig adkagent.RunConfig
	if stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}

	cb := &kagentCallbacks{
		appName:         appName,
		sessionService:  sessionService,
		skillsDirectory: skillsDir,
		log:             logger.WithName("a2a-executor"),
	}

	return adka2a.ExecutorConfig{
		RunnerConfig:          runnerConfig,
		RunConfig:             runConfig,
		BeforeExecuteCallback: cb.beforeExecute,
		AfterExecuteCallback:  cb.afterExecute,
		GenAIPartConverter:    makeGenAIPartConverter(subagentSessionIDs),
		A2APartConverter:      a2aPartConverter,
	}
}

// UserIDCallInterceptor returns an a2asrv.CallInterceptor that extracts the
// x-user-id HTTP header from the incoming request metadata and sets it as the
// authenticated user on the CallContext.
func UserIDCallInterceptor() a2asrv.CallInterceptor {
	return &userIDInterceptor{}
}

type userIDInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (u *userIDInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, error) {
	if callCtx == nil {
		return ctx, nil
	}
	meta := callCtx.RequestMeta()
	if meta == nil {
		return ctx, nil
	}
	vals, ok := meta.Get("x-user-id")
	if !ok || len(vals) == 0 || vals[0] == "" {
		return ctx, nil
	}
	// Set the authenticated user so downstream code picks up the real identity.
	callCtx.User = &a2asrv.AuthenticatedUser{UserName: vals[0]}
	return ctx, nil
}

// kagentCallbacks holds the state shared by the kagent executor callbacks.
type kagentCallbacks struct {
	appName         string
	sessionService  *session.KAgentSessionService
	skillsDirectory string
	log             logr.Logger
}

// beforeExecute sets up tracing, creates the session with session_name if
// needed, initializes skills, and processes any inbound HITL decision.
func (cb *kagentCallbacks) beforeExecute(ctx context.Context, reqCtx *a2asrv.RequestContext) (context.Context, error) {
	userID := "A2A_USER_" + reqCtx.ContextID
	// Don't use the synthetic ID if the user is authenticated.
	if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
		if callCtx.User != nil && callCtx.User.Name() != "" {
			userID = callCtx.User.Name()
		}
	}
	sessionID := reqCtx.ContextID

	cb.log.Info("BeforeExecute",
		"taskID", reqCtx.TaskID,
		"contextID", reqCtx.ContextID,
		"appName", cb.appName,
		"userID", userID,
	)

	spanAttributes := map[string]string{
		"kagent.user_id":         userID,
		"gen_ai.task.id":         string(reqCtx.TaskID),
		"gen_ai.conversation.id": sessionID,
	}
	if cb.appName != "" {
		spanAttributes["kagent.app_name"] = cb.appName
	}
	ctx = telemetry.SetKAgentSpanAttributes(ctx, spanAttributes)

	if cb.skillsDirectory != "" && sessionID != "" {
		if _, err := skills.InitializeSessionPath(sessionID, cb.skillsDirectory); err != nil {
			cb.log.V(1).Info("Skills session path init failed (continuing)",
				"error", err, "sessionID", sessionID)
		}
	}

	if cb.sessionService != nil {
		sess, err := cb.sessionService.GetSession(ctx, cb.appName, userID, sessionID)
		if err != nil {
			return ctx, fmt.Errorf("failed to lookup session %s: %w", sessionID, err)
		}
		if sess == nil {
			sessionName := extractSessionName(reqCtx.Message)
			state := make(map[string]any)
			if sessionName != "" {
				state[StateKeySessionName] = sessionName
			}
			// Propagate x-kagent-source so the session is tagged in the DB.
			if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
				if meta := callCtx.RequestMeta(); meta != nil {
					if vals, ok := meta.Get("x-kagent-source"); ok && len(vals) > 0 && vals[0] != "" {
						state[StateKeySource] = vals[0]
					}
				}
			}
			if err = cb.sessionService.CreateSession(ctx, cb.appName, userID, state, sessionID); err != nil {
				return ctx, fmt.Errorf("failed to create session: %w", err)
			}
		}
	}

	return ctx, nil
}

// afterExecute handles HITL enrichment for input_required states.
// The ADK executor already populates adk_* metadata on the final event.
func (cb *kagentCallbacks) afterExecute(ctx adka2a.ExecutorContext, finalEvent *a2atype.TaskStatusUpdateEvent, err error) error {
	if finalEvent == nil {
		return nil
	}

	state := finalEvent.Status.State
	cb.log.Info("AfterExecute", "sessionID", ctx.SessionID(), "state", state, "error", err)

	return nil
}

// ---------------------------------------------------------------------------
// Part converters
// ---------------------------------------------------------------------------

// makeGenAIPartConverter returns a GenAIPartConverter that converts GenAI parts
// to A2A parts and stamps kagent_subagent_session_id onto function_call
// DataParts for tools in subagentSessionIDs.
func makeGenAIPartConverter(subagentSessionIDs map[string]string) adka2a.GenAIPartConverter {
	return func(ctx context.Context, adkEvent *adksession.Event, part *genai.Part) (a2atype.Part, error) {
		var longRunningToolIDs []string
		if adkEvent != nil {
			longRunningToolIDs = adkEvent.LongRunningToolIDs
		}
		a2aPart, err := adka2a.ToA2APart(part, longRunningToolIDs)
		if err != nil {
			return nil, err
		}
		if isEmptyDataPart(a2aPart) {
			return nil, nil
		}
		// Stamp subagent_session_id onto function_call DataParts so the UI
		// can start polling the subagent session while the tool is running.
		if len(subagentSessionIDs) > 0 {
			stampSubagentSessionID(a2aPart, subagentSessionIDs)
		}
		return a2aPart, nil
	}
}

// stampSubagentSessionID adds kagent_subagent_session_id to the metadata of a
// function_call DataPart if the tool name is in subagentSessionIDs.
// Port of event_converter.py:_process_subagent_session_id().
func stampSubagentSessionID(part a2atype.Part, subagentSessionIDs map[string]string) {
	dp := asDataPart(part)
	if dp == nil || dp.Metadata == nil {
		return
	}
	partType, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataTypeKey)
	if partType != A2ADataPartMetadataTypeFunctionCall {
		return
	}
	toolName, _ := dp.Data[PartKeyName].(string)
	if toolName == "" {
		return
	}
	if sessionID, ok := subagentSessionIDs[toolName]; ok && sessionID != "" {
		dp.Metadata[GetKAgentMetadataKey("subagent_session_id")] = sessionID
	}
}

// a2aPartConverter converts inbound A2A parts to GenAI parts.
//
// DataParts with kagent_type metadata are converted explicitly (function_call /
// function_response).  DataParts with no recognised metadata — including HITL
// decision parts like {decision_type: "approve"} — are dropped (return nil).
//
// Dropping unrecognised DataParts mirrors Python's convert_a2a_part_to_genai_part
// which returns None for DataParts without a known kagent_type, causing them to
// be absent from the GenAI content that handleInputRequired inspects.  Without
// this, the ADK's toGenAIDataPart turns the decision DataPart into a text part,
// handleInputRequired finds no FunctionResponse matching the pending
// adk_request_confirmation FunctionCall, and immediately returns input_required
// again — leaving the UI stuck in "submitting decision".
func a2aPartConverter(ctx context.Context, a2aEvent a2atype.Event, part a2atype.Part) (*genai.Part, error) {
	dp := asDataPart(part)
	if dp == nil {
		// Text and file parts: delegate to ADK default.
		return adka2a.ToGenAIPart(part)
	}

	// DataPart with kagent_type metadata: convert explicitly.
	if dp.Metadata != nil {
		if _, has := dp.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)]; has {
			return convertDataPartToGenAI(dp, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
		}
	}

	// DataPart with adk_type metadata (produced by the ADK itself): delegate.
	if dp.Metadata != nil {
		if _, has := dp.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey)]; has {
			return adka2a.ToGenAIPart(part)
		}
	}

	// DataPart with no recognised type metadata (e.g. {decision_type: "approve"}).
	// Drop it — returning nil excludes it from the GenAI content, matching Python.
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isLongRunningDataPart(dp a2atype.DataPart) bool {
	if dp.Metadata == nil {
		return false
	}
	val, ok := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataIsLongRunningKey)
	if !ok {
		return false
	}
	lr, _ := val.(bool)
	return lr
}

// convertDataPartToGenAI converts a DataPart with a type metadata key
// (either adk_type or kagent_type) back to GenAI for inbound message processing.
func convertDataPartToGenAI(p *a2atype.DataPart, typeKey string) (*genai.Part, error) {
	if p == nil {
		return nil, nil
	}
	partType, _ := p.Metadata[typeKey].(string)
	switch partType {
	case A2ADataPartMetadataTypeFunctionCall:
		name, _ := p.Data[PartKeyName].(string)
		funcArgs, _ := p.Data[PartKeyArgs].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionCall(name, funcArgs)
			if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionCall.ID = id
			}
			return genaiPart, nil
		}
	case A2ADataPartMetadataTypeFunctionResponse:
		name, _ := p.Data[PartKeyName].(string)
		response, _ := p.Data[PartKeyResponse].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionResponse(name, response)
			if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionResponse.ID = id
			}
			return genaiPart, nil
		}
	}
	return adka2a.ToGenAIPart(p)
}

// extractSessionName extracts session name from the first text part of a message.
func extractSessionName(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	for _, part := range message.Parts {
		if tp, ok := part.(a2atype.TextPart); ok && tp.Text != "" {
			if len(tp.Text) > sessionNameMaxLength {
				return tp.Text[:sessionNameMaxLength] + "..."
			}
			return tp.Text
		}
	}
	return ""
}
