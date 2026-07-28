package a2a

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"os"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/skills"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
)

const (
	defaultSkillsDirectory = "/skills"
	envSkillsFolder        = "KAGENT_SKILLS_FOLDER"
	sessionNameMaxLength   = 20
)

// KAgentExecutorConfig holds the configuration for KAgentExecutor
type KAgentExecutorConfig struct {
	RunnerConfig    runner.Config
	SessionService  adksession.Service
	Stream          bool
	AppName         string
	SkillsDirectory string
	Logger          logr.Logger
}

// KAgentExecutor implements a2asrv.AgentExecutor
type KAgentExecutor struct {
	runnerConfig    runner.Config
	sessionService  adksession.Service
	stream          bool
	appName         string
	skillsDirectory string
	logger          logr.Logger
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
		runnerConfig:    cfg.RunnerConfig,
		sessionService:  cfg.SessionService,
		stream:          cfg.Stream,
		appName:         cfg.AppName,
		skillsDirectory: skillsDir,
		logger:          cfg.Logger.WithName("kagent-executor"),
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

func (u *userIDInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if callCtx == nil {
		return ctx, nil, nil
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return ctx, nil, nil
	}
	vals, ok := meta.Get("x-user-id")
	if !ok || len(vals) == 0 || vals[0] == "" {
		return ctx, nil, nil
	}
	// Set the authenticated user so downstream code picks up the real identity.
	callCtx.User = a2asrv.NewAuthenticatedUser(vals[0], nil)
	return ctx, nil, nil
}

// newAgentMessage builds an agent message stamped with the request's context
// and task ids. A2A allows omitting these (the task is the canonical carrier),
// but stamping them lets consumers that flatten task.history into standalone
// messages key each message to its task without backfilling. Mirrors the Python
// kagent-adk event converter.
func newAgentMessage(reqCtx *a2asrv.ExecutorContext, parts ...*a2atype.Part) *a2atype.Message {
	msg := a2atype.NewMessage(a2atype.MessageRoleAgent, parts...)
	msg.ContextID = reqCtx.ContextID
	msg.TaskID = reqCtx.TaskID
	return msg
}

// newAgentStatusEvent builds a working TaskStatusUpdateEvent whose agent message
// carries the given parts, the given metadata, and the request's context/task
// ids (via newAgentMessage). The message and event share the same metadata map,
// matching the executor's emission paths. This is the per-event seam where a
// streamed agent message is turned into an emitted (and persisted) event, so the
// id stamping here is what the send guard relies on when it later flattens
// task.history.
func newAgentStatusEvent(reqCtx *a2asrv.ExecutorContext, parts a2atype.ContentParts, meta map[string]any) *a2atype.TaskStatusUpdateEvent {
	msg := newAgentMessage(reqCtx, parts...)
	msg.Metadata = meta
	statusEv := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
	statusEv.Metadata = meta
	return statusEv
}

// Execute implements a2asrv.AgentExecutor.
// It follows the Python _handle_request pattern: set up session, handle HITL,
// convert inbound message, run the agent loop, and emit A2A events.
func (e *KAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if reqCtx.Message == nil {
			yield(nil, fmt.Errorf("A2A request message cannot be nil"))
			return
		}

		// 1. Derive userID / sessionID.
		userID := "A2A_USER_" + reqCtx.ContextID
		if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
			if callCtx.User != nil && callCtx.User.Name != "" {
				userID = callCtx.User.Name
			}
		}
		sessionID := reqCtx.ContextID

		ctx = withBearerToken(ctx)
		ctx = auth.WithUserID(ctx, userID)

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

		telemetry.SetMessageMetadataAttributes(ctx, reqCtx.Message.Metadata)

		// 3. Initialize skills session path.
		if e.skillsDirectory != "" && sessionID != "" {
			if _, err := skills.InitializeSessionPath(sessionID, e.skillsDirectory); err != nil {
				e.logger.V(1).Info("Skills session path init failed (continuing)",
					"error", err, "sessionID", sessionID)
			}
		}

		// 4. Create / lookup session via sessionService.
		if e.sessionService != nil {
			var sess adksession.Session
			resp, err := e.sessionService.Get(ctx, &adksession.GetRequest{AppName: e.appName, UserID: userID, SessionID: sessionID})
			if err != nil {
				e.logger.V(1).Info("Session lookup failed, will create", "error", err, "sessionID", sessionID)
			} else if resp != nil {
				sess = resp.Session
			}
			if sess == nil {
				sessionName := extractSessionName(reqCtx.Message)
				state := make(map[string]any)
				if sessionName != "" {
					state[StateKeySessionName] = sessionName
				}
				// Propagate x-kagent-source so the session is tagged in the DB.
				if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
					if meta := callCtx.ServiceParams(); meta != nil {
						if vals, ok := meta.Get("x-kagent-source"); ok && len(vals) > 0 && vals[0] != "" {
							state[StateKeySource] = vals[0]
						}
					}
				}
				if _, err := e.sessionService.Create(ctx, &adksession.CreateRequest{
					AppName:   e.appName,
					UserID:    userID,
					State:     state,
					SessionID: sessionID,
				}); err != nil {
					yield(nil, fmt.Errorf("failed to create session: %w", err))
					return
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
			yield(nil, fmt.Errorf("inbound message conversion failed: %w", err))
			return
		}

		// 8. Create runner.
		r, err := runner.New(e.runnerConfig)
		if err != nil {
			yield(nil, fmt.Errorf("failed to create runner: %w", err))
			return
		}

		// 9. Emit initial events.
		if reqCtx.StoredTask == nil {
			if !yield(a2atype.NewSubmittedTask(reqCtx, reqCtx.Message), nil) {
				return
			}
		} else if ExtractDecisionFromMessage(reqCtx.Message) != "" {
			// a2a-go appends incoming message to task history before executor runs.
			// Remove the pre-appended copy and emit one decision status event.
			dropPreAppendedDecisionFromHistory(reqCtx.StoredTask, reqCtx.Message)
			decision := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, reqCtx.Message)
			if !yield(decision, nil) {
				return
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
		if !yield(working, nil) {
			return
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
				if adkEvent.ErrorCode != "" {
					errMsg := newAgentMessage(reqCtx,
						a2atype.NewTextPart(fmt.Sprintf("LLM error: %s %s", adkEvent.ErrorCode, adkEvent.ErrorMessage)))
					failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
					failed.Metadata = eventMeta
					yield(failed, nil)
					return
				}
				continue
			}

			if adkEvent.ErrorCode != "" {
				errMsg := newAgentMessage(reqCtx,
					a2atype.NewTextPart(fmt.Sprintf("LLM error: %s %s", adkEvent.ErrorCode, adkEvent.ErrorMessage)))
				failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
				failed.Metadata = eventMeta
				yield(failed, nil)
				return
			}

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
					statusEv := newAgentStatusEvent(reqCtx, textOnly, mirrorMeta)
					if !yield(statusEv, nil) {
						return
					}
				}
			} else {
				if len(hitlParts) == 0 {
					statusEv := newAgentStatusEvent(reqCtx, a2aParts, maps.Clone(eventMeta))
					if !yield(statusEv, nil) {
						return
					}
					lastNonPartialParts = a2aParts
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
			errMsg := newAgentMessage(reqCtx, a2atype.NewTextPart(runErr.Error()))
			failed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, errMsg)
			failed.Metadata = finalMeta
			yield(failed, nil)
			return
		}

		if len(hitlParts) > 0 {
			// input_required: the agent is waiting for HITL decisions.
			hitlMsg := newAgentMessage(reqCtx, hitlParts...)
			inputRequired := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateInputRequired, hitlMsg)
			inputRequired.Metadata = finalMeta
			yield(inputRequired, nil)
			return
		}

		// Final artifact update with lastChunk=true (if we have parts) and final completed status update (no message payload).
		if len(lastNonPartialParts) > 0 {
			finalArtifact := a2atype.NewArtifactEvent(reqCtx, lastNonPartialParts...)
			finalArtifact.LastChunk = true
			if !yield(finalArtifact, nil) {
				return
			}
		}

		completed := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil)
		completed.Metadata = finalMeta
		yield(completed, nil)
	}
}

// Cancel implements a2asrv.AgentExecutor.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil)
		yield(event, nil)
	}
}

// extractSessionName extracts session name from the first text part of a message.
func extractSessionName(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	for _, part := range message.Parts {
		if part == nil {
			continue
		}
		if text := part.Text(); text != "" {
			if len(text) > sessionNameMaxLength {
				return text[:sessionNameMaxLength] + "..."
			}
			return text
		}
	}
	return ""
}

// withBearerToken extracts the Bearer token from the incoming A2A request's
// Authorization header and stores it in ctx for API key passthrough.
func withBearerToken(ctx context.Context) context.Context {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return ctx
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return ctx
	}
	vals, ok := meta.Get("authorization")
	if !ok || len(vals) == 0 || vals[0] == "" {
		return ctx
	}
	parts := strings.Fields(strings.TrimSpace(vals[0]))
	if len(parts) >= 2 && strings.EqualFold(parts[0], "Bearer") {
		return context.WithValue(ctx, models.BearerTokenKey, parts[1])
	}
	return ctx
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
