package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/kagent-dev/kagent/go/adk/pkg/temporal"
	"github.com/nats-io/nats.go"
)

// TemporalExecutor implements a2asrv.AgentExecutor by starting Temporal
// workflows instead of running the agent synchronously. NATS streaming events
// are forwarded to the A2A event queue for SSE delivery.
type TemporalExecutor struct {
	client     *temporal.Client
	config     temporal.TemporalConfig
	natsConn   *nats.Conn
	agentName  string // K8s agent name for Temporal workflow/task queue naming
	appName    string // __NS__-encoded app name for session/DB lookups
	configJSON []byte // serialized AgentConfig for workflow input
	log        logr.Logger
}

var _ a2asrv.AgentExecutor = (*TemporalExecutor)(nil)

// NewTemporalExecutor creates an executor that delegates to Temporal workflows.
// agentName is the Kubernetes agent name (e.g., "istio-agent") used for Temporal naming.
// appName is the encoded identifier (e.g., "kagent__NS__istio_agent") used for session/DB lookups.
func NewTemporalExecutor(
	client *temporal.Client,
	cfg temporal.TemporalConfig,
	natsConn *nats.Conn,
	agentName string,
	appName string,
	configJSON []byte,
	logger logr.Logger,
) *TemporalExecutor {
	return &TemporalExecutor{
		client:     client,
		config:     cfg,
		natsConn:   natsConn,
		agentName:  agentName,
		appName:    appName,
		configJSON: configJSON,
		log:        logger.WithName("temporal-executor"),
	}
}

// Execute starts a Temporal workflow for the given A2A request, subscribes to
// NATS for real-time streaming events, and forwards them to the A2A event queue.
func (e *TemporalExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	if reqCtx.Message == nil {
		return fmt.Errorf("A2A request message cannot be nil")
	}

	sessionID := reqCtx.ContextID
	userID := "A2A_USER_" + sessionID

	msgBytes, err := json.Marshal(reqCtx.Message)
	if err != nil {
		return fmt.Errorf("failed to marshal A2A message: %w", err)
	}

	req := &temporal.ExecutionRequest{
		SessionID:   sessionID,
		UserID:      userID,
		AgentName:   e.appName,
		Message:     msgBytes,
		Config:      e.configJSON,
		NATSSubject: streaming.SubjectForAgent(e.agentName, sessionID),
	}

	// Write submitted status.
	submitted := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateSubmitted, nil)
	if err := queue.Write(ctx, submitted); err != nil {
		return fmt.Errorf("failed to write submitted status: %w", err)
	}

	// Subscribe to NATS for streaming events AND completion tracking before starting workflow.
	// Both must be set up before signaling to avoid race conditions.
	completionCh := make(chan *temporal.ExecutionResult, 1)
	if e.natsConn != nil {
		var once sync.Once
		sub, subErr := e.natsConn.Subscribe(req.NATSSubject, func(msg *nats.Msg) {
			var event streaming.StreamEvent
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				return
			}
			if event.Type == streaming.EventTypeCompletion {
				var result temporal.ExecutionResult
				if err := json.Unmarshal([]byte(event.Data), &result); err == nil {
					select {
					case completionCh <- &result:
					default:
					}
				}
				return
			}
			e.forwardStreamEvent(ctx, reqCtx, queue, &event, &once)
		})
		if subErr != nil {
			e.log.Error(subErr, "Failed to subscribe to NATS, continuing without streaming", "subject", req.NATSSubject)
		} else {
			defer func() { _ = sub.Unsubscribe() }()
		}
	}

	// Write working status.
	working := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil)
	if err := queue.Write(ctx, working); err != nil {
		return fmt.Errorf("failed to write working status: %w", err)
	}

	// Signal-with-start: starts workflow if not running, or signals existing one.
	run, err := e.client.ExecuteAgent(ctx, req, e.config)
	if err != nil {
		failMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: fmt.Sprintf("Failed to start workflow: %v", err)})
		failEvent := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, failMsg)
		failEvent.Final = true
		_ = queue.Write(ctx, failEvent)
		return fmt.Errorf("failed to signal-with-start temporal workflow: %w", err)
	}

	e.log.Info("Workflow signaled", "workflowID", run.GetID(), "runID", run.GetRunID(), "sessionID", sessionID)

	// Wait for the completion event via NATS.
	// The workflow publishes a completion event after processing each message,
	// so we don't need to wait for the entire session workflow to end.
	select {
	case result := <-completionCh:
		return e.writeFinalStatus(ctx, reqCtx, queue, result)
	case <-ctx.Done():
		failMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "Request context cancelled"})
		failEvent := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, failMsg)
		failEvent.Final = true
		_ = queue.Write(ctx, failEvent)
		return ctx.Err()
	}
}

// Cancel sends a cancellation for the workflow associated with the task.
func (e *TemporalExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	cancelMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "Task cancelled"})
	cancelEvent := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, cancelMsg)
	cancelEvent.Final = true
	return queue.Write(ctx, cancelEvent)
}

// forwardStreamEvent converts a NATS streaming event to an A2A status update event.
func (e *TemporalExecutor) forwardStreamEvent(
	ctx context.Context,
	reqCtx *a2asrv.RequestContext,
	queue eventqueue.Queue,
	event *streaming.StreamEvent,
	sentWorking *sync.Once,
) {
	switch event.Type {
	case streaming.EventTypeToken:
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: event.Data})
		status := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
		if err := queue.Write(ctx, status); err != nil {
			e.log.V(1).Info("Failed to forward token event", "error", err)
		}

	case streaming.EventTypeToolStart:
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent,
			a2atype.DataPart{Data: map[string]any{"tool": event.Data, "status": "started"}})
		status := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
		if err := queue.Write(ctx, status); err != nil {
			e.log.V(1).Info("Failed to forward tool start event", "error", err)
		}

	case streaming.EventTypeToolEnd:
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent,
			a2atype.DataPart{Data: map[string]any{"tool": event.Data, "status": "completed"}})
		status := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, msg)
		if err := queue.Write(ctx, status); err != nil {
			e.log.V(1).Info("Failed to forward tool end event", "error", err)
		}

	case streaming.EventTypeApprovalRequest:
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: event.Data})
		status := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateInputRequired, msg)
		if err := queue.Write(ctx, status); err != nil {
			e.log.V(1).Info("Failed to forward approval request event", "error", err)
		}

	case streaming.EventTypeError:
		e.log.V(1).Info("Stream error event", "data", event.Data)
	}
}

// writeFinalStatus maps an ExecutionResult to the appropriate final A2A status event.
func (e *TemporalExecutor) writeFinalStatus(
	ctx context.Context,
	reqCtx *a2asrv.RequestContext,
	queue eventqueue.Queue,
	result *temporal.ExecutionResult,
) error {
	var state a2atype.TaskState
	var msg *a2atype.Message

	switch result.Status {
	case "completed":
		state = a2atype.TaskStateCompleted
		text := "Task completed"
		if len(result.Response) > 0 {
			text = string(result.Response)
		}
		msg = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: text})

	case "rejected":
		state = a2atype.TaskStateCanceled
		reason := "Rejected"
		if result.Reason != "" {
			reason = "Rejected: " + result.Reason
		}
		msg = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: reason})

	case "failed":
		state = a2atype.TaskStateFailed
		reason := "Workflow failed"
		if result.Reason != "" {
			reason = result.Reason
		}
		msg = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: reason})

	default:
		state = a2atype.TaskStateCompleted
		msg = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "Task completed"})
	}

	finalEvent := a2atype.NewStatusUpdateEvent(reqCtx, state, msg)
	finalEvent.Final = true
	return queue.Write(ctx, finalEvent)
}
