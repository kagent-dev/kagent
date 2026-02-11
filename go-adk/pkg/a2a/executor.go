package a2a

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/taskstore"
	"github.com/kagent-dev/kagent/go-adk/pkg/telemetry"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	// Default skills directory
	defaultSkillsDirectory = "/skills"

	// Environment variable for skills directory
	envSkillsFolder = "KAGENT_SKILLS_FOLDER"

	// Session name truncation length
	sessionNameMaxLength = 20
)

// A2aAgentExecutorConfig holds configuration for the executor.
type A2aAgentExecutorConfig struct {
	Stream           bool
	ExecutionTimeout time.Duration
}

// A2aAgentExecutor handles the execution of an agent against an A2A request.
type A2aAgentExecutor struct {
	Runner          *runner.Runner
	Config          A2aAgentExecutorConfig
	SessionService  session.SessionService
	TaskStore       *taskstore.KAgentTaskStore
	AppName         string
	SkillsDirectory string
}

// NewA2aAgentExecutor creates a new A2aAgentExecutor.
func NewA2aAgentExecutor(runner *runner.Runner, config A2aAgentExecutorConfig, sessionService session.SessionService, taskStore *taskstore.KAgentTaskStore, appName string) *A2aAgentExecutor {
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = model.DefaultExecutionTimeout
	}
	skillsDir := os.Getenv(envSkillsFolder)
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}
	return &A2aAgentExecutor{
		Runner:          runner,
		Config:          config,
		SessionService:  sessionService,
		TaskStore:       taskStore,
		AppName:         appName,
		SkillsDirectory: skillsDir,
	}
}

// Execute runs the agent and publishes updates to the event queue.
func (e *A2aAgentExecutor) Execute(ctx context.Context, req *protocol.SendMessageParams, queue EventQueue, taskID, contextID string) error {
	log := logr.FromContextOrDiscard(ctx)

	if req == nil {
		return fmt.Errorf("A2A request cannot be nil")
	}

	// 1. Extract user_id and session_id from request
	userID, sessionID := ExtractUserAndSessionID(req, contextID)

	// 2. Set kagent span attributes for tracing
	spanAttributes := map[string]string{
		"kagent.user_id":         userID,
		"gen_ai.task.id":         taskID,
		"gen_ai.conversation.id": sessionID,
	}
	if e.AppName != "" {
		spanAttributes["kagent.app_name"] = e.AppName
	}
	ctx = telemetry.SetKAgentSpanAttributes(ctx, spanAttributes)

	// 3. Prepare session (get or create)
	sess, err := e.prepareSession(ctx, userID, sessionID, &req.Message)
	if err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// Initialize session path for skills (matching Python implementation)
	if e.SkillsDirectory != "" && sessionID != "" {
		if _, err := skills.GetSessionPath(sessionID, e.SkillsDirectory); err != nil {
			log.V(1).Info("Failed to initialize session path for skills (continuing)", "error", err, "sessionID", sessionID, "skillsDirectory", e.SkillsDirectory)
		}
	}

	// 4. Send "submitted" status
	err = queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateSubmitted,
			Message:   &req.Message,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false,
	})
	if err != nil {
		return err
	}

	// 5. Convert A2A message to genai.Content directly
	genaiContent, err := convertProtocolMessageToGenAIContent(&req.Message)
	if err != nil {
		return e.sendFailure(ctx, queue, taskID, contextID, fmt.Sprintf("failed to convert message: %v", err))
	}
	if genaiContent == nil || len(genaiContent.Parts) == 0 {
		return e.sendFailure(ctx, queue, taskID, contextID, "message has no content")
	}

	// 6. Build RunConfig
	runConfig := adkagent.RunConfig{}
	if e.Config.Stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}

	// 7. Append system event before run (matches Python _handle_request)
	if e.SessionService != nil && sess != nil {
		if appendErr := e.SessionService.AppendFirstSystemEvent(ctx, sess); appendErr != nil {
			log.Error(appendErr, "Failed to append system event (continuing)", "sessionID", sess.ID)
		}
	}

	// 8. Start execution with timeout. Use WithoutCancel so that the execution
	// is not cancelled when the incoming request context is cancelled.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.Config.ExecutionTimeout)
	defer cancel()
	ctx = execCtx

	// 9. Send "working" status
	err = queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateWorking,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false,
		Metadata: map[string]any{
			"kagent_app_name":   e.AppName,
			"kagent_user_id":    userID,
			"kagent_session_id": sessionID,
		},
	})
	if err != nil {
		return err
	}

	// 10. Run — returns iter.Seq2, errors come through iterator
	eventSeq := e.Runner.Run(ctx, userID, sessionID, genaiContent, runConfig)

	// 11. Process events from iterator
	aggregator := NewTaskResultAggregator()
	for adkEvent, iterErr := range eventSeq {
		if ctx.Err() != nil {
			log.Info("Context cancelled during event processing", "error", ctx.Err())
			return ctx.Err()
		}

		if iterErr != nil {
			// Classify and send error as A2A event
			errorMsg, errorCode := formatRunnerError(iterErr)
			errorEvent := CreateErrorA2AEvent(errorCode, errorMsg, taskID, contextID, e.AppName, userID, sessionID)
			if errorEvent != nil {
				aggregator.ProcessEvent(errorEvent)
				if enqErr := queue.EnqueueEvent(ctx, errorEvent); enqErr != nil {
					return enqErr
				}
			}
			continue
		}

		if adkEvent == nil {
			continue
		}

		isPartial := adkEvent.Partial
		a2aEvents := ConvertADKEventToA2AEvents(adkEvent, taskID, contextID, e.AppName, userID, sessionID)
		for _, a2aEvent := range a2aEvents {
			if !isPartial {
				aggregator.ProcessEvent(a2aEvent)
			}
			if enqErr := queue.EnqueueEvent(ctx, a2aEvent); enqErr != nil {
				return enqErr
			}
		}
	}

	// 12. Send final status update (matching Python's final event handling)
	finalState := aggregator.TaskState
	finalMessage := aggregator.TaskMessage

	// Publish the task result event - this is final
	if finalState == protocol.TaskStateWorking &&
		finalMessage != nil &&
		len(finalMessage.Parts) > 0 {
		lastChunk := true
		artifactEvent := &protocol.TaskArtifactUpdateEvent{
			Kind:      "artifact-update",
			TaskID:    taskID,
			ContextID: contextID,
			LastChunk: &lastChunk,
			Artifact: protocol.Artifact{
				ArtifactID: uuid.New().String(),
				Parts:      finalMessage.Parts,
			},
		}
		if err := queue.EnqueueEvent(ctx, artifactEvent); err != nil {
			return err
		}

		return queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
			Kind:      "status-update",
			TaskID:    taskID,
			ContextID: contextID,
			Status: protocol.TaskStatus{
				State:     protocol.TaskStateCompleted,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			Final: true,
		})
	}

	// Handle other final states
	if finalState == protocol.TaskStateWorking || finalState == protocol.TaskStateSubmitted {
		finalState = protocol.TaskStateFailed
		if finalMessage == nil || len(finalMessage.Parts) == 0 {
			finalMessage = &protocol.Message{
				MessageID: uuid.New().String(),
				Role:      protocol.MessageRoleAgent,
				Parts: []protocol.Part{
					protocol.NewTextPart("The agent finished execution unexpectedly without a final response."),
				},
			}
		}
	}

	return queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     finalState,
			Message:   finalMessage,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: true,
	})
}

// prepareSession gets or creates a session, similar to Python's _prepare_session
func (e *A2aAgentExecutor) prepareSession(ctx context.Context, userID, sessionID string, message *protocol.Message) (*session.Session, error) {
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
func extractSessionName(message *protocol.Message) string {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}

	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok && textPart.Text != "" {
			text := textPart.Text
			if len(text) > sessionNameMaxLength {
				return text[:sessionNameMaxLength] + "..."
			}
			return text
		}
	}
	return ""
}

// ExtractUserAndSessionID extracts user_id and session_id from the A2A request.
func ExtractUserAndSessionID(req *protocol.SendMessageParams, contextID string) (userID, sessionID string) {
	const userIDPrefix = "A2A_USER_"
	sessionID = contextID
	userID = userIDPrefix + contextID
	return userID, sessionID
}

func (e *A2aAgentExecutor) sendFailure(ctx context.Context, queue EventQueue, taskID, contextID, message string) error {
	errorMessage := message
	if len(message) > 0 {
		if mappedMsg := model.GetErrorMessage(message); mappedMsg != model.DefaultErrorMessage {
			errorMessage = mappedMsg
		}
	}

	return queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State: protocol.TaskStateFailed,
			Message: &protocol.Message{
				MessageID: uuid.New().String(),
				Role:      protocol.MessageRoleAgent,
				Parts: []protocol.Part{
					protocol.NewTextPart(errorMessage),
				},
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: true,
	})
}
