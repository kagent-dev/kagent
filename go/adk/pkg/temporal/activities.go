package temporal

import (
	"context"

	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	"github.com/nats-io/nats.go"
)

// Activities holds dependencies for all Temporal activity implementations.
type Activities struct {
	sessionSvc session.SessionService
	taskStore  *taskstore.KAgentTaskStore
	natsConn   *nats.Conn
	publisher  *streaming.StreamPublisher
}

// NewActivities creates a new Activities instance with the given dependencies.
func NewActivities(
	sessionSvc session.SessionService,
	taskStore *taskstore.KAgentTaskStore,
	natsConn *nats.Conn,
) *Activities {
	var publisher *streaming.StreamPublisher
	if natsConn != nil {
		publisher = streaming.NewStreamPublisher(natsConn)
	}
	return &Activities{
		sessionSvc: sessionSvc,
		taskStore:  taskStore,
		natsConn:   natsConn,
		publisher:  publisher,
	}
}

// LLMInvokeActivity executes a single LLM chat completion turn.
// Tokens are streamed to NATS as they arrive.
func (a *Activities) LLMInvokeActivity(ctx context.Context, req *LLMRequest) (*LLMResponse, error) {
	// TODO: implement in Step 3
	return &LLMResponse{Terminal: true}, nil
}

// ToolExecuteActivity executes a single MCP tool call.
// Publishes tool start/end events to NATS.
func (a *Activities) ToolExecuteActivity(ctx context.Context, req *ToolRequest) (*ToolResponse, error) {
	// TODO: implement in Step 3
	return &ToolResponse{ToolCallID: req.ToolCallID}, nil
}

// SessionActivity creates or retrieves a session.
func (a *Activities) SessionActivity(ctx context.Context, req *SessionRequest) (*SessionResponse, error) {
	// TODO: implement in Step 3
	return &SessionResponse{SessionID: req.SessionID, Created: true}, nil
}

// SaveTaskActivity persists an A2A task.
func (a *Activities) SaveTaskActivity(ctx context.Context, req *TaskSaveRequest) error {
	// TODO: implement in Step 3
	return nil
}

// AppendEventActivity appends an event to a session.
func (a *Activities) AppendEventActivity(ctx context.Context, req *AppendEventRequest) error {
	// TODO: implement in Step 3
	return nil
}
