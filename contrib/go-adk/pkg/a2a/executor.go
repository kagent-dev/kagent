package a2a

import (
	"context"
	"fmt"
	"os"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/telemetry"
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
func NewExecutorConfig(
	runnerConfig runner.Config,
	sessionService session.SessionService,
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
		GenAIPartConverter:    genAIPartConverter,
		A2APartConverter:      a2aPartConverter,
	}
}

// kagentCallbacks holds the state shared by the kagent executor callbacks.
type kagentCallbacks struct {
	appName         string
	sessionService  session.SessionService
	skillsDirectory string
	log             logr.Logger
}

// beforeExecute sets up tracing, creates the session with session_name if
// needed, initializes skills, and appends the system header event.
func (cb *kagentCallbacks) beforeExecute(ctx context.Context, reqCtx *a2asrv.RequestContext) (context.Context, error) {
	userID := "A2A_USER_" + reqCtx.ContextID
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
			cb.log.V(1).Info("Session lookup failed, will create", "error", err, "sessionID", sessionID)
			sess = nil
		}
		if sess == nil {
			sessionName := extractSessionName(reqCtx.Message)
			state := make(map[string]any)
			if sessionName != "" {
				state[StateKeySessionName] = sessionName
			}
			if _, err = cb.sessionService.CreateSession(ctx, cb.appName, userID, state, sessionID); err != nil {
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

	if state == a2atype.TaskStateInputRequired && finalEvent.Status.Message != nil {
		approvalRequests := extractApprovalRequestsFromA2AParts(finalEvent.Status.Message.Parts)
		if len(approvalRequests) > 0 {
			cb.log.Info("Enriching HITL message", "approvalCount", len(approvalRequests))
			finalEvent.Status.Message = BuildToolApprovalMessage(approvalRequests)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Part converters
// ---------------------------------------------------------------------------
// genAIPartConverter converts GenAI parts to A2A parts, filtering out
// empty DataParts produced by the ADK for streaming cleanup signals.
func genAIPartConverter(ctx context.Context, adkEvent *adksession.Event, part *genai.Part) (a2atype.Part, error) {
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

	return a2aPart, nil
}

// a2aPartConverter converts A2A parts to GenAI parts. DataParts with
// kagent_type metadata (from older sessions) are handled explicitly;
// all other parts (including adk_type) fall through to the ADK default.
func a2aPartConverter(ctx context.Context, a2aEvent a2atype.Event, part a2atype.Part) (*genai.Part, error) {
	if dp, ok := part.(a2atype.DataPart); ok && dp.Metadata != nil {
		if _, has := dp.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)]; has {
			return convertDataPartToGenAI(&dp, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
		}
	}
	return adka2a.ToGenAIPart(part)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractApprovalRequestsFromA2AParts extracts tool approval requests from
// A2A data parts that represent long-running function calls.
func extractApprovalRequestsFromA2AParts(parts a2atype.ContentParts) []ToolApprovalRequest {
	var requests []ToolApprovalRequest
	for _, part := range parts {
		dp, ok := part.(a2atype.DataPart)
		if !ok {
			continue
		}
		if !isLongRunningDataPart(dp) {
			continue
		}
		name, _ := dp.Data["name"].(string)
		if name == "" || name == requestEucFunctionCallName {
			continue
		}
		args, _ := dp.Data["args"].(map[string]any)
		id, _ := dp.Data["id"].(string)
		requests = append(requests, ToolApprovalRequest{
			Name: name,
			Args: args,
			ID:   id,
		})
	}
	return requests
}

// isLongRunningDataPart checks both kagent_is_long_running and adk_is_long_running
// metadata keys for backward compatibility.
func isLongRunningDataPart(dp a2atype.DataPart) bool {
	if dp.Metadata == nil {
		return false
	}
	if lr, ok := dp.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataIsLongRunningKey)].(bool); ok && lr {
		return true
	}
	if lr, ok := dp.Metadata[adka2a.ToA2AMetaKey("is_long_running")].(bool); ok && lr {
		return true
	}
	return false
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
