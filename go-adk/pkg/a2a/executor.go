package a2a

import (
	"context"
	"fmt"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/telemetry"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
)

const (
	defaultSkillsDirectory = "/skills"
	envSkillsFolder        = "KAGENT_SKILLS_FOLDER"
	sessionNameMaxLength   = 20
)

// KAgentExecutorConfig holds configuration for the executor.
type KAgentExecutorConfig struct {
	Stream           bool
	ExecutionTimeout time.Duration
}

// KAgentExecutor implements a2asrv.AgentExecutor and handles execution of an
// agent against an A2A request.
type KAgentExecutor struct {
	Runner          *runner.Runner
	Config          KAgentExecutorConfig
	SessionService  session.SessionService
	AppName         string
	SkillsDirectory string
}

// Compile-time check that KAgentExecutor implements a2asrv.AgentExecutor.
var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// NewKAgentExecutor creates a new KAgentExecutor.
func NewKAgentExecutor(runner *runner.Runner, sessionService session.SessionService, config KAgentExecutorConfig, appName string) *KAgentExecutor {
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = DefaultExecutionTimeout
	}
	skillsDir := os.Getenv(envSkillsFolder)
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}
	return &KAgentExecutor{
		Runner:          runner,
		Config:          config,
		SessionService:  sessionService,
		AppName:         appName,
		SkillsDirectory: skillsDir,
	}
}

// Execute runs the agent and publishes updates to the event queue.
func (e *KAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	log := logr.FromContextOrDiscard(ctx)

	if reqCtx.Message == nil {
		return fmt.Errorf("A2A request message cannot be nil")
	}

	// 1. Extract user_id and session_id
	userID := "A2A_USER_" + reqCtx.ContextID
	sessionID := reqCtx.ContextID

	// 2. Set kagent span attributes for tracing
	spanAttributes := map[string]string{
		"kagent.user_id":         userID,
		"gen_ai.task.id":         string(reqCtx.TaskID),
		"gen_ai.conversation.id": sessionID,
	}
	if e.AppName != "" {
		spanAttributes["kagent.app_name"] = e.AppName
	}
	ctx = telemetry.SetKAgentSpanAttributes(ctx, spanAttributes)

	// 3. If StoredTask is nil (new task), write submitted event
	if reqCtx.StoredTask == nil {
		event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateSubmitted, reqCtx.Message)
		if err := queue.Write(ctx, event); err != nil {
			return err
		}
	}

	// 4. Prepare session (get or create)
	sess, err := e.prepareSession(ctx, userID, sessionID, reqCtx.Message)
	if err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// Initialize session path for skills
	if e.SkillsDirectory != "" && sessionID != "" {
		if _, err := skills.InitializeSessionPath(sessionID, e.SkillsDirectory); err != nil {
			log.V(1).Info("Failed to initialize session path for skills (continuing)", "error", err, "sessionID", sessionID, "skillsDirectory", e.SkillsDirectory)
		}
	}

	// 5. Append system event before run
	if e.SessionService != nil && sess != nil {
		if appendErr := e.SessionService.AppendFirstSystemEvent(ctx, sess); appendErr != nil {
			log.Error(appendErr, "Failed to append system event (continuing)", "sessionID", sess.ID)
		}
	}

	// 6. Send "working" status with kagent metadata
	workingEvent := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil)
	workingEvent.Metadata = map[string]any{
		GetKAgentMetadataKey("app_name"):   e.AppName,
		GetKAgentMetadataKey("user_id"):    userID,
		GetKAgentMetadataKey("session_id"): sessionID,
	}
	if err := queue.Write(ctx, workingEvent); err != nil {
		return err
	}

	// 7. Convert A2A message to genai.Content
	genaiContent, err := convertA2AMessageToGenAIContent(reqCtx.Message)
	if err != nil {
		return e.sendFailure(ctx, reqCtx, queue, fmt.Sprintf("failed to convert message: %v", err))
	}
	if genaiContent == nil || len(genaiContent.Parts) == 0 {
		return e.sendFailure(ctx, reqCtx, queue, "message has no content")
	}

	// 8. Build RunConfig
	runConfig := adkagent.RunConfig{}
	if e.Config.Stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}

	// 9. Start execution with timeout. Use WithoutCancel so execution is not
	// cancelled when the incoming request context is cancelled.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.Config.ExecutionTimeout)
	defer cancel()
	ctx = execCtx

	// 10. Run — returns iter.Seq2, errors come through iterator
	eventSeq := e.Runner.Run(ctx, userID, sessionID, genaiContent, runConfig)

	// 11. Process events from iterator with inline aggregation
	finalState := a2atype.TaskStateWorking
	var finalMessage *a2atype.Message
	var accumulatedParts a2atype.ContentParts

	for adkEvent, iterErr := range eventSeq {
		if ctx.Err() != nil {
			log.Info("Context cancelled during event processing", "error", ctx.Err())
			return ctx.Err()
		}

		if iterErr != nil {
			errorMsg, errorCode := formatRunnerError(iterErr)
			errorEvent := CreateErrorA2AEvent(errorCode, errorMsg, reqCtx, e.AppName, userID, sessionID)
			if errorEvent != nil {
				finalState = a2atype.TaskStateFailed
				finalMessage = errorEvent.Status.Message
				if writeErr := queue.Write(ctx, errorEvent); writeErr != nil {
					return writeErr
				}
			}
			continue
		}

		if adkEvent == nil {
			continue
		}

		// Check for tool approval interrupt before normal conversion.
		// This produces a rich human-readable approval message with
		// structured interrupt data, mirroring the Python HITL handler.
		if !adkEvent.Partial {
			if approvalRequests := ExtractToolApprovalRequests(adkEvent); len(approvalRequests) > 0 {
				log.Info("Tool approval interrupt detected", "numRequests", len(approvalRequests))
				msg, err := HandleToolApprovalInterrupt(ctx, approvalRequests, reqCtx, queue, e.AppName)
				if err != nil {
					return err
				}
				if finalState != a2atype.TaskStateFailed &&
					finalState != a2atype.TaskStateAuthRequired {
					finalState = a2atype.TaskStateInputRequired
					finalMessage = msg
				}
				continue
			}
		}

		isPartial := adkEvent.Partial
		a2aEvents := ConvertADKEventToA2AEvents(adkEvent, reqCtx, e.AppName, userID, sessionID)
		for _, a2aEvent := range a2aEvents {
			if !isPartial {
				// Inline aggregation: track state from non-partial events
				if statusEvent, ok := a2aEvent.(*a2atype.TaskStatusUpdateEvent); ok {
					switch statusEvent.Status.State {
					case a2atype.TaskStateFailed:
						finalState = a2atype.TaskStateFailed
						finalMessage = statusEvent.Status.Message
					case a2atype.TaskStateAuthRequired:
						if finalState != a2atype.TaskStateFailed {
							finalState = a2atype.TaskStateAuthRequired
							finalMessage = statusEvent.Status.Message
						}
					case a2atype.TaskStateInputRequired:
						if finalState != a2atype.TaskStateFailed &&
							finalState != a2atype.TaskStateAuthRequired {
							finalState = a2atype.TaskStateInputRequired
							finalMessage = statusEvent.Status.Message
						}
					default:
						// TaskStateWorking: accumulate parts
						if finalState == a2atype.TaskStateWorking {
							if statusEvent.Status.Message != nil && len(statusEvent.Status.Message.Parts) > 0 {
								accumulatedParts = append(accumulatedParts, statusEvent.Status.Message.Parts...)
								finalMessage = a2atype.NewMessage(a2atype.MessageRoleAgent, accumulatedParts...)
							} else {
								finalMessage = statusEvent.Status.Message
							}
						}
					}
					// Override event state to "working" for intermediate events
					statusEvent.Status.State = a2atype.TaskStateWorking
				}
			}
			if writeErr := queue.Write(ctx, a2aEvent); writeErr != nil {
				return writeErr
			}
		}
	}

	// 12. Send final status update
	if finalState == a2atype.TaskStateWorking &&
		finalMessage != nil &&
		len(finalMessage.Parts) > 0 {
		// Emit artifact for the accumulated content
		artifactEvent := a2atype.NewArtifactEvent(reqCtx, finalMessage.Parts...)
		artifactEvent.LastChunk = true
		if err := queue.Write(ctx, artifactEvent); err != nil {
			return err
		}

		// Emit completed status
		completedEvent := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil)
		completedEvent.Final = true
		return queue.Write(ctx, completedEvent)
	}

	// Handle other final states
	if finalState == a2atype.TaskStateWorking || finalState == a2atype.TaskStateSubmitted {
		finalState = a2atype.TaskStateFailed
		if finalMessage == nil || len(finalMessage.Parts) == 0 {
			finalMessage = a2atype.NewMessage(a2atype.MessageRoleAgent,
				a2atype.TextPart{Text: "The agent finished execution unexpectedly without a final response."},
			)
		}
	}

	event := a2atype.NewStatusUpdateEvent(reqCtx, finalState, finalMessage)
	event.Final = true
	return queue.Write(ctx, event)
}

// Cancel is called when the client requests the agent to stop working on a task.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// prepareSession gets or creates a session.
func (e *KAgentExecutor) prepareSession(ctx context.Context, userID, sessionID string, message *a2atype.Message) (*session.Session, error) {
	if e.SessionService == nil {
		return &session.Session{
			ID:      sessionID,
			UserID:  userID,
			AppName: e.AppName,
			State:   make(map[string]any),
		}, nil
	}

	sess, err := e.SessionService.GetSession(ctx, e.AppName, userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if sess == nil {
		sessionName := extractSessionName(message)
		state := make(map[string]any)
		if sessionName != "" {
			state[StateKeySessionName] = sessionName
		}
		sess, err = e.SessionService.CreateSession(ctx, e.AppName, userID, state, sessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
	}

	return sess, nil
}

// extractSessionName extracts session name from message.
func extractSessionName(message *a2atype.Message) string {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}
	for _, part := range message.Parts {
		if textPart, ok := part.(a2atype.TextPart); ok && textPart.Text != "" {
			text := textPart.Text
			if len(text) > sessionNameMaxLength {
				return text[:sessionNameMaxLength] + "..."
			}
			return text
		}
	}
	return ""
}

func (e *KAgentExecutor) sendFailure(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue, message string) error {
	msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: message})
	event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, msg)
	event.Final = true
	return queue.Write(ctx, event)
}
