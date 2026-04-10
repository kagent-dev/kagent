package a2a

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/skills"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	adkapi "github.com/kagent-dev/kagent/go/api/adk"
	"go.opentelemetry.io/otel/attribute"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
)

const (
	defaultSkillsDirectory = "/skills"
	envSkillsFolder        = "KAGENT_SKILLS_FOLDER"
	sessionNameMaxLength   = 20
)

// KAgentExecutorConfig holds the configuration for KAgentExecutor
type KAgentExecutorConfig struct {
	RunnerConfig       runner.Config
	SubagentSessionIDs map[string]string
	SessionService     *session.KAgentSessionService
	Stream             bool
	AppName            string
	SkillsDirectory    string
	Logger             logr.Logger
	RetryPolicy        *adkapi.RetryPolicyConfig
}

// KAgentExecutor implements a2asrv.AgentExecutor
type KAgentExecutor struct {
	runnerConfig       runner.Config
	subagentSessionIDs map[string]string
	sessionService     *session.KAgentSessionService
	stream             bool
	appName            string
	skillsDirectory    string
	logger             logr.Logger
	retryPolicy        *adkapi.RetryPolicyConfig
}

var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// NewKAgentExecutor creates a KAgentExecutor from config
func NewKAgentExecutor(cfg KAgentExecutorConfig) *KAgentExecutor {
	skillsDir := cfg.SkillsDirectory
	if skillsDir == "" {
		skillsDir = os.Getenv(envSkillsFolder)
	}
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}
	return &KAgentExecutor{
		runnerConfig:       cfg.RunnerConfig,
		subagentSessionIDs: cfg.SubagentSessionIDs,
		sessionService:     cfg.SessionService,
		stream:             cfg.Stream,
		appName:            cfg.AppName,
		skillsDirectory:    skillsDir,
		logger:             cfg.Logger.WithName("kagent-executor"),
		retryPolicy:        cfg.RetryPolicy,
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

// computeRetryDelay calculates the delay for a retry attempt using exponential backoff.
func computeRetryDelay(attempt int, policy *adkapi.RetryPolicyConfig) time.Duration {
	delay := policy.InitialRetryDelay * float64(int(1)<<attempt)
	if policy.MaxRetryDelay != nil && delay > *policy.MaxRetryDelay {
		delay = *policy.MaxRetryDelay
	}
	return time.Duration(delay * float64(time.Second))
}

// executeWithRetry runs fn with optional retry on failure.
// If policy is nil or MaxRetries is 0, fn is called once.
// Context cancellation is never retried.
func executeWithRetry(ctx context.Context, policy *adkapi.RetryPolicyConfig, fn func(ctx context.Context) error) error {
	maxAttempts := 1
	if policy != nil {
		maxAttempts += policy.MaxRetries
	}

	var lastErr error
	for attempt := range maxAttempts {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		// Never retry context cancellation
		if ctx.Err() != nil || errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded) {
			return lastErr
		}

		if attempt+1 < maxAttempts {
			delay := computeRetryDelay(attempt, policy)
			logr.FromContextOrDiscard(ctx).Info("Request failed, retrying",
				"attempt", attempt+1,
				"maxAttempts", maxAttempts,
				"delay", delay,
				"error", lastErr,
			)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

// Execute implements a2asrv.AgentExecutor.
func (e *KAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	return executeWithRetry(ctx, e.retryPolicy, func(ctx context.Context) error {
		return e.executeOnce(ctx, reqCtx, queue)
	})
}

// executeOnce performs a single attempt at handling an A2A request.
// It follows the Python _handle_request pattern: set up session, handle HITL,
// convert inbound message, run the agent loop, and emit A2A events.
func (e *KAgentExecutor) executeOnce(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	if reqCtx.Message == nil {
		return fmt.Errorf("A2A request message cannot be nil")
	}

	// 1. Derive userID / sessionID.
	userID := "A2A_USER_" + reqCtx.ContextID
	if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
		if callCtx.User != nil && callCtx.User.Name() != "" {
			userID = callCtx.User.Name()
		}
	}
	sessionID := reqCtx.ContextID

	e.logger.Info("Execute",
		"taskID", reqCtx.TaskID,
		"contextID", reqCtx.ContextID,
		"appName", e.appName,
		"userID", userID,
	)

	// 2. Set up telemetry span attributes.
	spanAttributes := map[string]string{
		"kagent.user_id":         userID,
		"gen_ai.task.id":         string(reqCtx.TaskID),
		"gen_ai.conversation.id": sessionID,
	}
	if e.appName != "" {
		spanAttributes["kagent.app_name"] = e.appName
	}
	ctx = telemetry.SetKAgentSpanAttributes(ctx, spanAttributes)
	ctx, invocationSpan := telemetry.StartInvocationSpan(ctx)
	defer invocationSpan.End()

	// 3. Initialize skills session path.
	if e.skillsDirectory != "" && sessionID != "" {
		if _, err := skills.InitializeSessionPath(sessionID, e.skillsDirectory); err != nil {
			e.logger.V(1).Info("Skills session path init failed (continuing)",
				"error", err, "sessionID", sessionID)
		}
	}

	// 4. Create / lookup session via sessionService.
	if e.sessionService != nil {
		sess, err := e.sessionService.GetSession(ctx, e.appName, userID, sessionID)
		if err != nil {
			e.logger.V(1).Info("Session lookup failed, will create", "error", err, "sessionID", sessionID)
			sess = nil
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
			if err = e.sessionService.CreateSession(ctx, e.appName, userID, state, sessionID); err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
		}
	}

	// 5. Detect HITL decision and build the resume message if needed.
	inboundMessage := reqCtx.Message
	if resumeMessage := BuildResumeHITLMessage(reqCtx.StoredTask, inboundMessage); resumeMessage != nil {
		inboundMessage = resumeMessage
	}

	// 6. Convert inbound message to *genai.Content using kagent a2aPartConverter.
	content, err := messageToGenAIContent(ctx, inboundMessage)
	if err != nil {
		return fmt.Errorf("inbound message conversion failed: %w", err)
	}

	// 7. Use pre-built subagent session ID map (built by runner bundle).
	subagentSessionIDs := e.subagentSessionIDs

	// 8. Create runner.
	r, err := runner.New(e.runnerConfig)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	// 9. Emit initial events.
	if reqCtx.StoredTask == nil {
		// New task — emit submitted with the user's message
		submitted := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateSubmitted, reqCtx.Message)
		if err := queue.Write(ctx, submitted); err != nil {
			return fmt.Errorf("failed to write submitted event: %w", err)
		}
	} else if ExtractDecisionFromMessage(reqCtx.Message) != "" {
		// a2a-go appends incoming message to task history before executor runs.
		// See https://github.com/a2aproject/a2a-go/blob/v0.3.13/a2asrv/agentexec.go#L188
		// Remove the pre-appended copy and emit one decision status event.
		dropPreAppendedDecisionFromHistory(reqCtx.StoredTask, reqCtx.Message)
		decision := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, reqCtx.Message)
		if err := queue.Write(ctx, decision); err != nil {
			return fmt.Errorf("failed to write HITL decision status event: %w", err)
		}
	}

	// Base metadata carried on every event (app_name, user_id, session_id).
	baseMeta := map[string]any{
		adka2a.ToA2AMetaKey("app_name"):   e.appName,
		adka2a.ToA2AMetaKey("user_id"):    userID,
		adka2a.ToA2AMetaKey("session_id"): sessionID,
	}

	working := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil)
	working.Metadata = maps.Clone(baseMeta)
	if err := queue.Write(ctx, working); err != nil {
		return fmt.Errorf("failed to write working event: %w", err)
	}

	// 10. Run the agent event loop.
	var runConfig adkagent.RunConfig
	if e.stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}

	// State tracked across the event loop.
	var (
		invocationID        string
		lastNonPartialParts a2atype.ContentParts
		hitlParts           a2atype.ContentParts
		runErr              error
	)

	for adkEvent, adkErr := range r.Run(ctx, userID, sessionID, content, runConfig) {
		if adkErr != nil {
			runErr = adkErr
			break
		}
		if adkEvent == nil {
			continue
		}

		// Track invocation ID from the first event that has one.
		if adkEvent.InvocationID != "" && invocationID == "" {
			invocationID = adkEvent.InvocationID
			invocationSpan.SetAttributes(attribute.String("gcp.vertex.agent.invocation_id", invocationID))
		}

		// Build per-event metadata (inherits baseMeta + adds invocation_id, usage etc.).
		eventMeta := buildEventMeta(baseMeta, adkEvent)

		// Convert GenAI parts → A2A parts (with kagent stamping).
		if adkEvent.Content == nil || len(adkEvent.Content.Parts) == 0 {
			// Events with no content carry metadata only; still track invocationID/usage.
			// Check for LLM error.
			if adkEvent.ErrorCode != "" {
				errMsg := a2atype.NewMessage(a2atype.MessageRoleAgent,
					a2atype.TextPart{Text: fmt.Sprintf("LLM error: %s %s", adkEvent.ErrorCode, adkEvent.ErrorMessage)})
				failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
				failed.Final = true
				failed.Metadata = eventMeta
				return queue.Write(ctx, failed)
			}
			continue
		}

		// Check for LLM error (even with content present).
		if adkEvent.ErrorCode != "" {
			errMsg := a2atype.NewMessage(a2atype.MessageRoleAgent,
				a2atype.TextPart{Text: fmt.Sprintf("LLM error: %s %s", adkEvent.ErrorCode, adkEvent.ErrorMessage)})
			failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
			failed.Final = true
			failed.Metadata = eventMeta
			return queue.Write(ctx, failed)
		}

		// Convert parts.
		var a2aParts a2atype.ContentParts
		for _, genaiPart := range adkEvent.Content.Parts {
			if genaiPart == nil {
				continue
			}
			a2aPart, err := adka2a.ToA2APart(genaiPart, adkEvent.LongRunningToolIDs)
			if err != nil {
				continue
			}
			if isEmptyDataPart(a2aPart) {
				continue
			}
			// Stamp kagent_subagent_session_id onto function_call DataParts.
			if len(subagentSessionIDs) > 0 {
				a2aPart = stampSubagentSessionID(a2aPart, subagentSessionIDs)
			}
			a2aParts = append(a2aParts, a2aPart)
		}

		// Collect HITL (input_required) parts from LongRunningToolIDs.
		isHITLEvent := len(adkEvent.LongRunningToolIDs) > 0
		if isHITLEvent {
			hitlParts = append(hitlParts, a2aParts...)
		}

		if len(a2aParts) == 0 {
			continue
		}

		if adkEvent.Partial {
			// Partial event: emit as working status (text-only) for UI streaming.
			// Note: Go ADK executor uses TaskArtifactUpdateEvent for partial events,
			// so we don't need to emit a separate partial artifact update.
			// However, this is done here in order to match the Python executor's behavior.
			// Go ADK executor also uses different A2A response formats than Python ADK.
			textOnly := filterTextParts(a2aParts)
			if len(textOnly) > 0 {
				mirrorMeta := maps.Clone(eventMeta)
				mirrorMeta[adka2a.ToA2AMetaKey("partial")] = true
				msg := a2atype.NewMessage(a2atype.MessageRoleAgent, textOnly...)
				msg.Metadata = mirrorMeta
				statusEv := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
				statusEv.Metadata = mirrorMeta
				if err := queue.Write(ctx, statusEv); err != nil {
					return fmt.Errorf("failed to write partial status event: %w", err)
				}
			}
		} else {
			mirrorParts := a2aParts
			if len(hitlParts) == 0 {
				// Only mirror when not accumulating HITL parts (those go into input_required).
				msg := a2atype.NewMessage(a2atype.MessageRoleAgent, mirrorParts...)
				msg.Metadata = maps.Clone(eventMeta)
				statusEv := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
				statusEv.Metadata = maps.Clone(eventMeta)
				if err := queue.Write(ctx, statusEv); err != nil {
					return fmt.Errorf("failed to write mirror status event: %w", err)
				}
				lastNonPartialParts = mirrorParts
			}
		}

		// Break on confirmation events that have long-running tool IDs.
		if isHITLEvent {
			break
		}
	}

	// 11. Emit final event.
	finalMeta := maps.Clone(baseMeta)
	if invocationID != "" {
		finalMeta[adka2a.ToA2AMetaKey("invocation_id")] = invocationID
	}

	if runErr != nil {
		errMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: runErr.Error()})
		failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
		failed.Final = true
		failed.Metadata = finalMeta
		return queue.Write(ctx, failed)
	}

	if len(hitlParts) > 0 {
		// input_required: the agent is waiting for HITL decisions.
		hitlMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, hitlParts...)
		inputRequired := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateInputRequired, hitlMsg)
		inputRequired.Final = true
		inputRequired.Metadata = finalMeta
		return queue.Write(ctx, inputRequired)
	}

	// Final artifact update with lastChunk=true (if we have parts) and final completed status update (no message payload).
	if len(lastNonPartialParts) > 0 {
		finalArtifact := a2atype.NewArtifactEvent(reqCtx, lastNonPartialParts...)
		finalArtifact.LastChunk = true
		if err := queue.Write(ctx, finalArtifact); err != nil {
			return fmt.Errorf("failed to write final artifact event: %w", err)
		}
	}

	completed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil)
	completed.Final = true
	completed.Metadata = finalMeta
	return queue.Write(ctx, completed)
}

// Cancel implements a2asrv.AgentExecutor.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
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

// dropPreAppendedDecisionFromHistory removes a pre-appended HITL decision
// message inserted by a2a-go before executor invocation.
func dropPreAppendedDecisionFromHistory(task *a2atype.Task, incoming *a2atype.Message) {
	if task == nil || incoming == nil || len(task.History) == 0 {
		return
	}
	last := task.History[len(task.History)-1]
	if last == nil || last.ID != incoming.ID {
		return
	}
	if ExtractDecisionFromMessage(last) == "" {
		return
	}
	task.History = task.History[:len(task.History)-1]
}
