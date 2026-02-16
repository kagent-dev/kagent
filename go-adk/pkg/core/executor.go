package core

import (
	"context"
	"fmt"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/taskstore"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/telemetry"
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
	ConvertEvents   a2a.ConvertEventsFunc
	IsPartial       a2a.IsPartialFunc
	Config          A2aAgentExecutorConfig
	SessionService  session.SessionService
	TaskStore       *taskstore.KAgentTaskStore
	AppName         string
	SkillsDirectory string
	Logger          logr.Logger
}

// NewA2aAgentExecutorWithLogger creates a new A2aAgentExecutor with a logger.
func NewA2aAgentExecutorWithLogger(runner Runner, convertEvents a2a.ConvertEventsFunc, isPartial a2a.IsPartialFunc, config A2aAgentExecutorConfig, sessionService session.SessionService, taskStore *taskstore.KAgentTaskStore, appName string, logger logr.Logger) *A2aAgentExecutor {
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = a2a.DefaultExecutionTimeout
	}
	// Get skills directory from environment (matching Python's KAGENT_SKILLS_FOLDER)
	skillsDir := os.Getenv(envSkillsFolder)
	if skillsDir == "" {
		skillsDir = defaultSkillsDirectory
	}
	return &A2aAgentExecutor{
		Runner:          runner,
		ConvertEvents:   convertEvents,
		IsPartial:       isPartial,
		Config:          config,
		SessionService:  sessionService,
		TaskStore:       taskStore,
		AppName:         appName,
		SkillsDirectory: skillsDir,
		Logger:          logger,
	}
}

// Execute runs the agent and publishes updates to the event queue.
func (e *A2aAgentExecutor) Execute(ctx context.Context, req *a2atype.MessageSendParams, queue a2a.EventQueue, taskID, contextID string) error {
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
	session, err := e.prepareSession(ctx, userID, sessionID, req.Message)
	if err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// 4. Initialize session path for skills (matching Python implementation)
	if e.SkillsDirectory != "" && sessionID != "" {
		if _, err := skills.InitializeSessionPath(sessionID, e.SkillsDirectory); err != nil {
			if e.Logger.GetSink() != nil {
				e.Logger.V(1).Info("Failed to initialize session path for skills (continuing)", "error", err, "sessionID", sessionID, "skillsDirectory", e.SkillsDirectory)
			}
		}
	}

	// 5. Send "submitted" status
	err = queue.EnqueueEvent(ctx, &a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(taskID),
		ContextID: contextID,
		Status: a2atype.TaskStatus{
			State:     a2atype.TaskStateSubmitted,
			Message:   req.Message,
			Timestamp: timePtr(time.Now()),
		},
		Final: false,
	})
	if err != nil {
		return err
	}

	// 6. Prepare run arguments
	runArgs := a2a.ConvertA2ARequestToRunArgs(req, userID, sessionID)
	streamingMode := "NONE"
	if e.Config.Stream {
		streamingMode = "SSE"
	}
	if runArgs[a2a.ArgKeyRunConfig] == nil {
		runArgs[a2a.ArgKeyRunConfig] = make(map[string]interface{})
	}
	if runConfig, ok := runArgs[a2a.ArgKeyRunConfig].(map[string]interface{}); ok {
		runConfig[a2a.RunConfigKeyStreamingMode] = streamingMode
	}
	// Add session service and session to runArgs so runner can save events to history
	runArgs[a2a.ArgKeySessionService] = e.SessionService
	runArgs[a2a.ArgKeySession] = session
	runArgs[a2a.ArgKeyAppName] = e.AppName

	// 7. Append system event before run (matches Python _handle_request: append_event before runner.run_async)
	if e.SessionService != nil && session != nil {
		if appendErr := e.SessionService.AppendFirstSystemEvent(ctx, session); appendErr != nil && e.Logger.GetSink() != nil {
			e.Logger.Error(appendErr, "Failed to append system event (continuing)", "sessionID", session.ID)
		}
	}

	// 8. Start execution with timeout. Use WithoutCancel so that the execution
	// (and thus the runner / MCP tool calls) is not cancelled when the incoming
	// request context is cancelled (e.g. HTTP client disconnect or short server
	// timeout). Long-running MCP tools get up to ExecutionTimeout to complete.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.Config.ExecutionTimeout)
	defer cancel()
	ctx = execCtx

	// 9. Send "working" status
	err = queue.EnqueueEvent(ctx, &a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(taskID),
		ContextID: contextID,
		Status: a2atype.TaskStatus{
			State:     a2atype.TaskStateWorking,
			Timestamp: timePtr(time.Now()),
		},
		Final: false,
		Metadata: map[string]interface{}{
			a2a.MetadataKeyAppName:       e.AppName,
			a2a.MetadataKeyUserIDFull:    userID,
			a2a.MetadataKeySessionIDFull: sessionID,
		},
	})
	if err != nil {
		return err
	}

	aggregator := taskstore.NewTaskResultAggregator()
	eventChan, err := e.Runner.Run(ctx, runArgs)
	if err != nil {
		return e.sendFailure(ctx, queue, taskID, contextID, err.Error())
	}

	// 10. Process events from the runner
	defer func() {
		for range eventChan {
		}
	}()

	cc := a2a.ConversionContext{
		TaskID: taskID, ContextID: contextID,
		AppName: e.AppName, UserID: userID, SessionID: sessionID,
	}
	for internalEvent := range eventChan {
		if ctx.Err() != nil {
			if e.Logger.GetSink() != nil {
				e.Logger.Info("Context cancelled during event processing", "error", ctx.Err())
			}
			return ctx.Err()
		}

		isPartial := e.IsPartial(internalEvent)
		a2aEvents := e.ConvertEvents(internalEvent, cc)
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

	// 11. Send final status update (matching Python's final event handling)
	finalState := aggregator.TaskState
	finalMessage := aggregator.TaskMessage

	// Publish the task result event - this is final
	// (matching Python: if task_result_aggregator.task_state == TaskState.working
	//  and task_result_aggregator.task_status_message is not None
	//  and task_result_aggregator.task_status_message.parts)
	if finalState == a2atype.TaskStateWorking &&
		finalMessage != nil &&
		len(finalMessage.Parts) > 0 {
		// If task is still working properly, publish the artifact update event as
		// the final result according to a2a protocol (matching Python)
		artifactEvent := &a2atype.TaskArtifactUpdateEvent{
			TaskID:    a2atype.TaskID(taskID),
			ContextID: contextID,
			LastChunk: true,
			Artifact: &a2atype.Artifact{
				ID:    a2atype.NewArtifactID(),
				Parts: finalMessage.Parts,
			},
		}
		if err := queue.EnqueueEvent(ctx, artifactEvent); err != nil {
			return err
		}

		// Publish the final status update event (matching Python)
		return queue.EnqueueEvent(ctx, &a2atype.TaskStatusUpdateEvent{
			TaskID:    a2atype.TaskID(taskID),
			ContextID: contextID,
			Status: a2atype.TaskStatus{
				State:     a2atype.TaskStateCompleted,
				Timestamp: timePtr(time.Now()),
			},
			Final: true,
		})
	}

	// Handle other final states
	// If the loop finished but we are still in a non-terminal state, it's an error
	// (matching Python: if final_state in (TaskState.working, TaskState.submitted))
	if finalState == a2atype.TaskStateWorking || finalState == a2atype.TaskStateSubmitted {
		finalState = a2atype.TaskStateFailed
		if finalMessage == nil || len(finalMessage.Parts) == 0 {
			finalMessage = &a2atype.Message{
				ID:   uuid.New().String(),
				Role: a2atype.MessageRoleAgent,
				Parts: a2atype.ContentParts{
					a2atype.TextPart{Text: "The agent finished execution unexpectedly without a final response."},
				},
			}
		}
	}

	// Send final status update with message
	return queue.EnqueueEvent(ctx, &a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(taskID),
		ContextID: contextID,
		Status: a2atype.TaskStatus{
			State:     finalState,
			Message:   finalMessage,
			Timestamp: timePtr(time.Now()),
		},
		Final: true,
	})
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// prepareSession gets or creates a session, similar to Python's _prepare_session
func (e *A2aAgentExecutor) prepareSession(ctx context.Context, userID, sessionID string, message *a2atype.Message) (*session.Session, error) {
	if e.SessionService == nil {
		// Return a minimal session if no session service is configured
		return &session.Session{
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
			state[a2a.StateKeySessionName] = sessionName
		}

		session, err = e.SessionService.CreateSession(ctx, e.AppName, userID, state, sessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
	}

	return session, nil
}

// extractSessionName extracts session name from message, similar to Python implementation
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

// ExtractUserAndSessionID extracts user_id and session_id from the A2A request.
// The session_id is derived from the context_id, and user_id defaults to "A2A_USER_" + context_id.
// This matches the Python implementation's _get_user_id behavior.
func ExtractUserAndSessionID(req *a2atype.MessageSendParams, contextID string) (userID, sessionID string) {
	const userIDPrefix = "A2A_USER_"

	// Use context_id as session_id (like Python version)
	sessionID = contextID

	// Try to extract user_id from request metadata or use default
	// In Python: _get_user_id gets it from call_context.user.user_name or defaults to f"A2A_USER_{context_id}"
	userID = userIDPrefix + contextID
	// When the A2A protocol exposes call_context.user, use it here for userID.

	return userID, sessionID
}

func (e *A2aAgentExecutor) sendFailure(ctx context.Context, queue a2a.EventQueue, taskID, contextID, message string) error {
	// Use GetErrorMessage if message looks like an error code
	// This provides user-friendly error messages when possible
	errorMessage := message
	if len(message) > 0 {
		// Check if message is a known error code
		if mappedMsg := genai.GetErrorMessage(message); mappedMsg != genai.DefaultErrorMessage {
			errorMessage = mappedMsg
		}
	}

	return queue.EnqueueEvent(ctx, &a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(taskID),
		ContextID: contextID,
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateFailed,
			Message: &a2atype.Message{
				ID:   uuid.New().String(),
				Role: a2atype.MessageRoleAgent,
				Parts: a2atype.ContentParts{
					a2atype.TextPart{Text: errorMessage},
				},
			},
			Timestamp: timePtr(time.Now()),
		},
		Final: true,
	})
}
