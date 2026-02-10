package core

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
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

// Runner is an interface for running the agent logic.
type Runner interface {
	Run(ctx context.Context, args map[string]interface{}) (<-chan interface{}, error)
}

// A2aAgentExecutorConfig holds configuration for the executor.
type A2aAgentExecutorConfig struct {
	Stream           bool
	ExecutionTimeout time.Duration
}

// A2aAgentExecutor handles the execution of an agent against an A2A request.
type A2aAgentExecutor struct {
	Runner          Runner
	Converter       EventConverter
	Config          A2aAgentExecutorConfig
	SessionService  SessionService
	TaskStore       *KAgentTaskStore
	AppName         string
	SkillsDirectory string
	Logger          logr.Logger
}

// NewA2aAgentExecutorWithLogger creates a new A2aAgentExecutor with a logger.
func NewA2aAgentExecutorWithLogger(runner Runner, converter EventConverter, config A2aAgentExecutorConfig, sessionService SessionService, taskStore *KAgentTaskStore, appName string, logger logr.Logger) *A2aAgentExecutor {
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = models.DefaultExecutionTimeout
	}
	// Get skills directory from environment (matching Python's KAGENT_SKILLS_FOLDER)
	skillsDir := os.Getenv(envSkillsFolder)
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}
	return &A2aAgentExecutor{
		Runner:          runner,
		Converter:       converter,
		Config:          config,
		SessionService:  sessionService,
		TaskStore:       taskStore,
		AppName:         appName,
		SkillsDirectory: skillsDir,
		Logger:          logger,
	}
}

// Execute runs the agent and publishes updates to the event queue.
func (e *A2aAgentExecutor) Execute(ctx context.Context, req *protocol.SendMessageParams, queue EventQueue, taskID, contextID string) error {
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
	ctx = SetKAgentSpanAttributes(ctx, spanAttributes)
	// Note: ClearKAgentSpanAttributes is not called in defer because the context
	// is local to this function and reassigning ctx in a defer doesn't affect
	// the original context. Span attributes are cleaned up when the context is done.

	// 3. Prepare session (get or create)
	session, err := e.prepareSession(ctx, userID, sessionID, &req.Message)
	if err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// 2.5. Initialize session path for skills (matching Python implementation)
	if e.SkillsDirectory != "" && sessionID != "" {
		if _, err := InitializeSessionPath(sessionID, e.SkillsDirectory); err != nil {
			// Log but continue: skills can still be accessed via absolute path
			if e.Logger.GetSink() != nil {
				e.Logger.V(1).Info("Failed to initialize session path for skills (continuing)", "error", err, "sessionID", sessionID, "skillsDirectory", e.SkillsDirectory)
			}
		}
	}

	// 3. Send "submitted" status if this is the first message for this task
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

	// 4. Prepare run arguments
	runArgs := ConvertA2ARequestToRunArgs(req, userID, sessionID)
	// Set streaming mode from executor config so ADK and model stream when config.stream is true
	streamingMode := "NONE"
	if e.Config.Stream {
		streamingMode = "SSE"
	}
	if runArgs[ArgKeyRunConfig] == nil {
		runArgs[ArgKeyRunConfig] = make(map[string]interface{})
	}
	if runConfig, ok := runArgs[ArgKeyRunConfig].(map[string]interface{}); ok {
		runConfig[RunConfigKeyStreamingMode] = streamingMode
	}
	// Add session service and session to runArgs so runner can save events to history
	runArgs[ArgKeySessionService] = e.SessionService
	runArgs[ArgKeySession] = session
	// App name must match executor's so runner's session lookup returns the same session (Python: runner.app_name)
	runArgs[ArgKeyAppName] = e.AppName

	// 4.5. Append system event before run (matches Python _handle_request: append_event before runner.run_async)
	if e.SessionService != nil && session != nil {
		if appendErr := e.SessionService.AppendFirstSystemEvent(ctx, session); appendErr != nil && e.Logger.GetSink() != nil {
			e.Logger.Error(appendErr, "Failed to append system event (continuing)", "sessionID", session.ID)
		}
	}

	// 5. Start execution with timeout. Use WithoutCancel so that the execution
	// (and thus the runner / MCP tool calls) is not cancelled when the incoming
	// request context is cancelled (e.g. HTTP client disconnect or short server
	// timeout). Long-running MCP tools get up to ExecutionTimeout to complete.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.Config.ExecutionTimeout)
	defer cancel()
	ctx = execCtx

	// 6. Send "working" status
	err = queue.EnqueueEvent(ctx, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateWorking,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false,
		Metadata: map[string]interface{}{
			"kagent_app_name":   e.AppName,
			"kagent_user_id":    userID,
			"kagent_session_id": sessionID,
		},
	})
	if err != nil {
		return err
	}

	aggregator := NewTaskResultAggregator()
	eventChan, err := e.Runner.Run(ctx, runArgs)
	if err != nil {
		return e.sendFailure(ctx, queue, taskID, contextID, err.Error())
	}

	// 7. Process events from the runner
	// Ensure channel is drained and closed properly (matching Python's async with aclosing)
	defer func() {
		// Drain any remaining events from channel if it wasn't closed
		for range eventChan {
			// Drain remaining events
		}
	}()

	for internalEvent := range eventChan {
		// Check for context cancellation at start of each iteration
		if ctx.Err() != nil {
			if e.Logger.GetSink() != nil {
				e.Logger.Info("Context cancelled during event processing", "error", ctx.Err())
			}
			return ctx.Err()
		}

		// Check if event is partial (matching Python: if not adk_event.partial)
		isPartial := e.Converter.IsPartialEvent(internalEvent)

		a2aEvents := e.Converter.ConvertEventToA2AEvents(internalEvent, taskID, contextID, e.AppName, userID, sessionID)
		for _, a2aEvent := range a2aEvents {
			// Only aggregate non-partial events to avoid duplicates from streaming chunks
			// Partial events are sent to frontend for display but not accumulated
			// (matching Python: if not adk_event.partial: task_result_aggregator.process_event(a2a_event))
			if !isPartial {
				aggregator.ProcessEvent(a2aEvent)
			}

			if err := queue.EnqueueEvent(ctx, a2aEvent); err != nil {
				return err
			}
		}

		// Do not append streamed events here. Matching Python: the executor only appends the system
		// event (header_update); streamed events are appended once by the runner layer (adk_runner
		// or the Google ADK session service). Appending here would duplicate persistence.
	}

	// 8. Send final status update (matching Python's final event handling)
	finalState := aggregator.TaskState
	finalMessage := aggregator.TaskMessage

	// Publish the task result event - this is final
	// (matching Python: if task_result_aggregator.task_state == TaskState.working
	//  and task_result_aggregator.task_status_message is not None
	//  and task_result_aggregator.task_status_message.parts)
	if finalState == protocol.TaskStateWorking &&
		finalMessage != nil &&
		len(finalMessage.Parts) > 0 {
		// If task is still working properly, publish the artifact update event as
		// the final result according to a2a protocol (matching Python)
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

		// Publish the final status update event (matching Python)
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
	// If the loop finished but we are still in a non-terminal state, it's an error
	// (matching Python: if final_state in (TaskState.working, TaskState.submitted))
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

	// Send final status update with message
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
func (e *A2aAgentExecutor) prepareSession(ctx context.Context, userID, sessionID string, message *protocol.Message) (*Session, error) {
	if e.SessionService == nil {
		// Return a minimal session if no session service is configured
		return &Session{
			ID:      sessionID,
			UserID:  userID,
			AppName: e.AppName,
			State:   make(map[string]interface{}),
		}, nil
	}

	// Try to get existing session
	session, err := e.SessionService.GetSession(ctx, e.AppName, userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Create new session if it doesn't exist
	if session == nil {
		// Extract session name from the first TextPart (like the Python version does)
		sessionName := extractSessionName(message)
		state := make(map[string]interface{})
		if sessionName != "" {
			state[StateKeySessionName] = sessionName
		}

		session, err = e.SessionService.CreateSession(ctx, e.AppName, userID, state, sessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
	}

	return session, nil
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
// The session_id is derived from the context_id, and user_id defaults to "A2A_USER_" + context_id.
// This matches the Python implementation's _get_user_id behavior.
func ExtractUserAndSessionID(req *protocol.SendMessageParams, contextID string) (userID, sessionID string) {
	const userIDPrefix = "A2A_USER_"

	// Use context_id as session_id (like Python version)
	sessionID = contextID

	// Try to extract user_id from request metadata or use default
	// In Python: _get_user_id gets it from call_context.user.user_name or defaults to f"A2A_USER_{context_id}"
	userID = userIDPrefix + contextID
	// When the A2A protocol exposes call_context.user, use it here for userID.

	return userID, sessionID
}

func (e *A2aAgentExecutor) sendFailure(ctx context.Context, queue EventQueue, taskID, contextID, message string) error {
	// Use GetErrorMessage if message looks like an error code
	// This provides user-friendly error messages when possible
	errorMessage := message
	if len(message) > 0 {
		// Check if message is a known error code
		if mappedMsg := genai.GetErrorMessage(message); mappedMsg != genai.DefaultErrorMessage {
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
