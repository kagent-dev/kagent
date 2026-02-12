package a2a

import (
	"context"
	"fmt"
	"os"
	"time"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/telemetry"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
)

const (
	// Default skills directory
	defaultSkillsDirectory = "/skills"

	// Environment variable for skills directory
	envSkillsFolder = "KAGENT_SKILLS_FOLDER"

	// Session name truncation length
	sessionNameMaxLength = 20
)

// KAgentExecutorConfig holds configuration for the executor.
type KAgentExecutorConfig struct {
	Stream           bool
	ExecutionTimeout time.Duration
}

// KAgentExecutor implements a2asrv.AgentExecutor and handles the execution
// of an agent against an A2A request.
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
		config.ExecutionTimeout = model.DefaultExecutionTimeout
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

	// 1. Extract user_id and session_id from request context
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
		event := a2aschema.NewStatusUpdateEvent(reqCtx, a2aschema.TaskStateSubmitted, reqCtx.Message)
		if err := queue.Write(ctx, event); err != nil {
			return err
		}
	}

	// 4. Prepare session (get or create)
	sess, err := e.prepareSession(ctx, userID, sessionID, reqCtx.Message)
	if err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// Initialize session path for skills (matching Python implementation)
	if e.SkillsDirectory != "" && sessionID != "" {
		if _, err := skills.GetSessionPath(sessionID, e.SkillsDirectory); err != nil {
			log.V(1).Info("Failed to initialize session path for skills (continuing)", "error", err, "sessionID", sessionID, "skillsDirectory", e.SkillsDirectory)
		}
	}

	// 5. Append system event before run (matches Python _handle_request)
	if e.SessionService != nil && sess != nil {
		if appendErr := e.SessionService.AppendFirstSystemEvent(ctx, sess); appendErr != nil {
			log.Error(appendErr, "Failed to append system event (continuing)", "sessionID", sess.ID)
		}
	}

	// 6. Send "working" status with kagent metadata
	workingEvent := a2aschema.NewStatusUpdateEvent(reqCtx, a2aschema.TaskStateWorking, nil)
	workingEvent.Metadata = map[string]any{
		"kagent_app_name":   e.AppName,
		"kagent_user_id":    userID,
		"kagent_session_id": sessionID,
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

	// 9. Start execution with timeout. Use WithoutCancel so that the execution
	// is not cancelled when the incoming request context is cancelled.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.Config.ExecutionTimeout)
	defer cancel()
	ctx = execCtx

	// 10. Run — returns iter.Seq2, errors come through iterator
	eventSeq := e.Runner.Run(ctx, userID, sessionID, genaiContent, runConfig)

	// 11. Process events from iterator
	// Track final state (inline aggregator logic since we're removing the separate aggregator)
	finalState := a2aschema.TaskStateWorking
	var finalMessage *a2aschema.Message
	var accumulatedParts a2aschema.ContentParts

	for adkEvent, iterErr := range eventSeq {
		if ctx.Err() != nil {
			log.Info("Context cancelled during event processing", "error", ctx.Err())
			return ctx.Err()
		}

		if iterErr != nil {
			// Classify and send error as A2A event
			errorMsg, errorCode := formatRunnerError(iterErr)
			errorEvent := CreateErrorA2AEvent(errorCode, errorMsg, reqCtx, e.AppName, userID, sessionID)
			if errorEvent != nil {
				// Track error state
				finalState = a2aschema.TaskStateFailed
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

		isPartial := adkEvent.Partial
		a2aEvents := ConvertADKEventToA2AEvents(adkEvent, reqCtx, e.AppName, userID, sessionID)
		for _, a2aEvent := range a2aEvents {
			if !isPartial {
				// Track state from this event (aggregator logic)
				if statusEvent, ok := a2aEvent.(*a2aschema.TaskStatusUpdateEvent); ok {
					switch statusEvent.Status.State {
					case a2aschema.TaskStateFailed:
						finalState = a2aschema.TaskStateFailed
						finalMessage = statusEvent.Status.Message
					case a2aschema.TaskStateAuthRequired:
						if finalState != a2aschema.TaskStateFailed {
							finalState = a2aschema.TaskStateAuthRequired
							finalMessage = statusEvent.Status.Message
						}
					case a2aschema.TaskStateInputRequired:
						if finalState != a2aschema.TaskStateFailed &&
							finalState != a2aschema.TaskStateAuthRequired {
							finalState = a2aschema.TaskStateInputRequired
							finalMessage = statusEvent.Status.Message
						}
					default:
						// TaskStateWorking or others: accumulate parts
						if finalState == a2aschema.TaskStateWorking {
							if statusEvent.Status.Message != nil && len(statusEvent.Status.Message.Parts) > 0 {
								accumulatedParts = append(accumulatedParts, statusEvent.Status.Message.Parts...)
								finalMessage = a2aschema.NewMessage(a2aschema.MessageRoleAgent, accumulatedParts...)
							} else {
								finalMessage = statusEvent.Status.Message
							}
						}
					}
					// Override event state to "working" for intermediate events
					statusEvent.Status.State = a2aschema.TaskStateWorking
				}
			}
			if writeErr := queue.Write(ctx, a2aEvent); writeErr != nil {
				return writeErr
			}
		}
	}

	// 12. Send final status update (matching Python's final event handling)
	if finalState == a2aschema.TaskStateWorking &&
		finalMessage != nil &&
		len(finalMessage.Parts) > 0 {
		// Emit artifact for the accumulated content
		artifactEvent := a2aschema.NewArtifactEvent(reqCtx, finalMessage.Parts...)
		artifactEvent.LastChunk = true
		if err := queue.Write(ctx, artifactEvent); err != nil {
			return err
		}

		// Emit completed status
		completedEvent := a2aschema.NewStatusUpdateEvent(reqCtx, a2aschema.TaskStateCompleted, nil)
		completedEvent.Final = true
		return queue.Write(ctx, completedEvent)
	}

	// Handle other final states
	if finalState == a2aschema.TaskStateWorking || finalState == a2aschema.TaskStateSubmitted {
		finalState = a2aschema.TaskStateFailed
		if finalMessage == nil || len(finalMessage.Parts) == 0 {
			finalMessage = a2aschema.NewMessage(a2aschema.MessageRoleAgent,
				&a2aschema.TextPart{Text: "The agent finished execution unexpectedly without a final response."},
			)
		}
	}

	event := a2aschema.NewStatusUpdateEvent(reqCtx, finalState, finalMessage)
	event.Final = true
	return queue.Write(ctx, event)
}

// Cancel is called when the client requests the agent to stop working on a task.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2aschema.NewStatusUpdateEvent(reqCtx, a2aschema.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// prepareSession gets or creates a session, similar to Python's _prepare_session
func (e *KAgentExecutor) prepareSession(ctx context.Context, userID, sessionID string, message *a2aschema.Message) (*session.Session, error) {
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

// extractSessionName extracts session name from message, similar to Python implementation
func extractSessionName(message *a2aschema.Message) string {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}

	for _, part := range message.Parts {
		if textPart, ok := part.(*a2aschema.TextPart); ok && textPart.Text != "" {
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
	errorMessage := message
	if len(message) > 0 {
		if mappedMsg := model.GetErrorMessage(message); mappedMsg != model.DefaultErrorMessage {
			errorMessage = mappedMsg
		}
	}

	msg := a2aschema.NewMessage(a2aschema.MessageRoleAgent, &a2aschema.TextPart{Text: errorMessage})
	event := a2aschema.NewStatusUpdateEvent(reqCtx, a2aschema.TaskStateFailed, msg)
	event.Final = true
	return queue.Write(ctx, event)
}
