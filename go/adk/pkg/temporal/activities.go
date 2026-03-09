package temporal

import (
	"context"
	"encoding/json"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	"github.com/nats-io/nats.go"
)

// ModelInvoker invokes an LLM model with the given config and conversation history.
// The onToken callback is called for each streamed token (may be nil if streaming is not needed).
// Config and history are JSON-encoded AgentConfig and conversation history respectively.
type ModelInvoker func(ctx context.Context, config []byte, history []byte, onToken func(string)) (*LLMResponse, error)

// ToolExecutor executes an MCP tool by name with the given JSON-encoded arguments.
// Returns the JSON-encoded result.
type ToolExecutor func(ctx context.Context, toolName string, args []byte) ([]byte, error)

// Activities holds dependencies for all Temporal activity implementations.
type Activities struct {
	sessionSvc   session.SessionService
	taskStore    *taskstore.KAgentTaskStore
	natsConn     *nats.Conn
	publisher    *streaming.StreamPublisher
	modelInvoker ModelInvoker
	toolExecutor ToolExecutor
}

// NewActivities creates a new Activities instance with the given dependencies.
func NewActivities(
	sessionSvc session.SessionService,
	taskStore *taskstore.KAgentTaskStore,
	natsConn *nats.Conn,
	modelInvoker ModelInvoker,
	toolExecutor ToolExecutor,
) *Activities {
	var publisher *streaming.StreamPublisher
	if natsConn != nil {
		publisher = streaming.NewStreamPublisher(natsConn)
	}
	return &Activities{
		sessionSvc:   sessionSvc,
		taskStore:    taskStore,
		natsConn:     natsConn,
		publisher:    publisher,
		modelInvoker: modelInvoker,
		toolExecutor: toolExecutor,
	}
}

// SessionActivity creates or retrieves a session.
// If the session already exists, it is returned. Otherwise, a new one is created.
func (a *Activities) SessionActivity(ctx context.Context, req *SessionRequest) (*SessionResponse, error) {
	if a.sessionSvc == nil {
		return nil, fmt.Errorf("session service is not configured")
	}

	// Try to get existing session first.
	sess, err := a.sessionSvc.GetSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err == nil && sess != nil {
		return &SessionResponse{SessionID: sess.ID, Created: false}, nil
	}

	// Create a new session.
	sess, err = a.sessionSvc.CreateSession(ctx, req.AppName, req.UserID, nil, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create session %s: %w", req.SessionID, err)
	}

	return &SessionResponse{SessionID: sess.ID, Created: true}, nil
}

// LLMInvokeActivity executes a single LLM chat completion turn.
// Tokens are streamed to NATS as they arrive.
func (a *Activities) LLMInvokeActivity(ctx context.Context, req *LLMRequest) (*LLMResponse, error) {
	if a.modelInvoker == nil {
		return nil, fmt.Errorf("model invoker is not configured")
	}

	// Build a token callback that publishes to NATS if available.
	var onToken func(string)
	if a.publisher != nil && req.NATSSubject != "" {
		onToken = func(token string) {
			event := streaming.NewStreamEvent(streaming.EventTypeToken, token)
			// Fire-and-forget: streaming errors are non-fatal.
			_ = a.publisher.PublishToken(req.NATSSubject, event)
		}
	}

	resp, err := a.modelInvoker(ctx, req.Config, req.History, onToken)
	if err != nil {
		// Publish error event to NATS if available.
		if a.publisher != nil && req.NATSSubject != "" {
			errEvent := streaming.NewStreamEvent(streaming.EventTypeError, err.Error())
			_ = a.publisher.PublishToolProgress(req.NATSSubject, errEvent)
		}
		return nil, fmt.Errorf("LLM invocation failed: %w", err)
	}

	return resp, nil
}

// ToolExecuteActivity executes a single MCP tool call.
// Publishes tool start/end events to NATS.
func (a *Activities) ToolExecuteActivity(ctx context.Context, req *ToolRequest) (*ToolResponse, error) {
	if a.toolExecutor == nil {
		return nil, fmt.Errorf("tool executor is not configured")
	}

	// Publish tool_start event.
	if a.publisher != nil && req.NATSSubject != "" {
		startEvent := streaming.NewStreamEvent(streaming.EventTypeToolStart, req.ToolName)
		_ = a.publisher.PublishToolProgress(req.NATSSubject, startEvent)
	}

	result, err := a.toolExecutor(ctx, req.ToolName, req.Args)

	// Publish tool_end event regardless of success/failure.
	if a.publisher != nil && req.NATSSubject != "" {
		var endData string
		if err != nil {
			endData = fmt.Sprintf("%s:error:%s", req.ToolName, err.Error())
		} else {
			endData = req.ToolName
		}
		endEvent := streaming.NewStreamEvent(streaming.EventTypeToolEnd, endData)
		_ = a.publisher.PublishToolProgress(req.NATSSubject, endEvent)
	}

	if err != nil {
		return &ToolResponse{
			ToolCallID: req.ToolCallID,
			Error:      err.Error(),
		}, nil // Return tool error in response, not as activity error (no retry).
	}

	return &ToolResponse{
		ToolCallID: req.ToolCallID,
		Result:     result,
	}, nil
}

// SaveTaskActivity persists an A2A task.
func (a *Activities) SaveTaskActivity(ctx context.Context, req *TaskSaveRequest) error {
	if a.taskStore == nil {
		return fmt.Errorf("task store is not configured")
	}

	var task a2atype.Task
	if err := json.Unmarshal(req.TaskData, &task); err != nil {
		return fmt.Errorf("failed to unmarshal task data: %w", err)
	}

	if err := a.taskStore.Save(ctx, &task); err != nil {
		return fmt.Errorf("failed to save task for session %s: %w", req.SessionID, err)
	}

	return nil
}

// PublishApprovalActivity publishes an HITL approval request to NATS.
// This is an activity (not workflow.SideEffect) because it needs the NATS connection,
// which is external I/O that cannot be performed inside a deterministic workflow.
func (a *Activities) PublishApprovalActivity(ctx context.Context, req *PublishApprovalRequest) error {
	if a.publisher == nil {
		// No NATS connection -- skip publishing. The workflow will still wait for the signal.
		return nil
	}

	approvalReq := &streaming.ApprovalRequest{
		WorkflowID: req.WorkflowID,
		RunID:      req.RunID,
		SessionID:  req.SessionID,
		Message:    req.Message,
	}
	// Fire-and-forget: if publishing fails, the signal can still be sent via HTTP API.
	_ = a.publisher.PublishApprovalRequest(req.NATSSubject, approvalReq)
	return nil
}

// AppendEventActivity appends an event to a session.
func (a *Activities) AppendEventActivity(ctx context.Context, req *AppendEventRequest) error {
	if a.sessionSvc == nil {
		return fmt.Errorf("session service is not configured")
	}

	// Unmarshal event from JSON to generic map for the session service.
	var event any
	if err := json.Unmarshal(req.Event, &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Get the session to pass to AppendEvent.
	sess, err := a.sessionSvc.GetSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session %s for event append: %w", req.SessionID, err)
	}
	if sess == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	if err := a.sessionSvc.AppendEvent(ctx, sess, event); err != nil {
		return fmt.Errorf("failed to append event to session %s: %w", req.SessionID, err)
	}

	return nil
}
