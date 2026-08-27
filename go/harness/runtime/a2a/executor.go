// Package a2a supervises Harness runtime turns behind kagent's private A2A
// service. Public Task persistence remains owned by the controller gateway.
package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// Runner is the execution capability consumed by the A2A supervisor.
type Runner interface {
	Run(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error)
}

// ContinuationStore persists the one native conversation owned by an Actor.
// A2A contexts identify controller history; they do not select native sessions.
type ContinuationStore interface {
	Load() (string, bool, error)
	Bind(continuationID string) error
}

type Executor struct {
	runner       Runner
	continuation ContinuationStore

	mu     sync.Mutex
	active *activeTask
}

type activeTask struct {
	taskID    a2atype.TaskID
	contextID string
	cancel    context.CancelFunc
	done      chan struct{}
}

type executionSink struct {
	reqCtx       *a2asrv.ExecutorContext
	yield        func(a2atype.Event, error) bool
	continuation ContinuationStore
	artifactID   a2atype.ArtifactID
}

var (
	errBusy         = errors.New("runtime actor already has an active task")
	errYieldStopped = errors.New("A2A event consumer stopped")
)

func New(runner Runner, continuation ContinuationStore) (*Executor, error) {
	if runner == nil || continuation == nil {
		return nil, fmt.Errorf("runner and continuation store are required")
	}
	return &Executor{runner: runner, continuation: continuation}, nil
}

func (e *Executor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		prompt, err := validateRequest(reqCtx)
		if err != nil {
			yield(nil, err)
			return
		}
		continuationID, _, err := e.continuation.Load()
		if err != nil {
			yield(nil, err)
			return
		}
		runCtx, cancel := context.WithCancel(ctx)
		active := &activeTask{taskID: reqCtx.TaskID, contextID: reqCtx.ContextID, cancel: cancel, done: make(chan struct{})}
		if !e.activate(active) {
			cancel()
			yield(nil, errBusy)
			return
		}
		var finishOnce sync.Once
		finish := func() {
			finishOnce.Do(func() {
				cancel()
				e.deactivate(active)
				close(active.done)
			})
		}
		defer finish()

		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}
		outcome, runErr := e.runner.Run(runCtx, runtime.Turn{
			Prompt: prompt, ContinuationID: continuationID,
		}, &executionSink{reqCtx: reqCtx, yield: yield, continuation: e.continuation})
		if errors.Is(runErr, errYieldStopped) {
			return
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return
		}
		if runErr != nil {
			finish()
			message := taskMessage(reqCtx, "Harness runtime execution failed")
			yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, message), nil)
			return
		}

		// Reap the runtime process and release the Actor's active-task slot before
		// publishing a terminal state. A client may submit its next turn as soon
		// as it observes this event.
		finish()
		if outcome.Failure == nil {
			yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil), nil)
			return
		}
		message := taskMessage(reqCtx, safeFailure(outcome.Failure.Message))
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, message), nil)
	}
}

func (s *executionSink) SessionStarted(event runtime.SessionStarted) error {
	if event.ContinuationID == "" {
		return fmt.Errorf("runtime continuation ID is required")
	}
	if err := s.continuation.Bind(event.ContinuationID); err != nil {
		return fmt.Errorf("persist runtime continuation: %w", err)
	}
	return nil
}

func (s *executionSink) TextDelta(event runtime.TextDelta) error {
	if event.Text == "" {
		return nil
	}
	var update *a2atype.TaskArtifactUpdateEvent
	if s.artifactID == "" {
		update = a2atype.NewArtifactEvent(s.reqCtx, a2atype.NewTextPart(event.Text))
		s.artifactID = update.Artifact.ID
	} else {
		update = a2atype.NewArtifactUpdateEvent(s.reqCtx, s.artifactID, a2atype.NewTextPart(event.Text))
	}
	if !s.yield(update, nil) {
		return errYieldStopped
	}
	return nil
}

func (s *executionSink) ToolCall(event runtime.ToolCall) error {
	message, err := toolCallMessage(s.reqCtx, event)
	if err != nil {
		return err
	}
	if !s.yield(a2atype.NewStatusUpdateEvent(s.reqCtx, a2atype.TaskStateWorking, message), nil) {
		return errYieldStopped
	}
	return nil
}

func (s *executionSink) ToolResult(event runtime.ToolResult) error {
	message, err := toolResultMessage(s.reqCtx, event)
	if err != nil {
		return err
	}
	if !s.yield(a2atype.NewStatusUpdateEvent(s.reqCtx, a2atype.TaskStateWorking, message), nil) {
		return errYieldStopped
	}
	return nil
}

func toolCallMessage(reqCtx *a2asrv.ExecutorContext, event runtime.ToolCall) (*a2atype.Message, error) {
	if event.ID == "" || event.Name == "" {
		return nil, fmt.Errorf("runtime tool call requires an ID and name")
	}
	args := event.Arguments
	if args == nil {
		args = map[string]any{}
	}
	return toolActivityMessage(reqCtx, "function_call", map[string]any{
		"id": event.ID, "name": event.Name, "args": args,
	}), nil
}

func toolResultMessage(reqCtx *a2asrv.ExecutorContext, event runtime.ToolResult) (*a2atype.Message, error) {
	if event.ID == "" || event.Name == "" {
		return nil, fmt.Errorf("runtime tool result requires an ID and name")
	}
	response := map[string]any{"result": event.Result}
	if event.IsError {
		response["isError"] = true
	}
	return toolActivityMessage(reqCtx, "function_response", map[string]any{
		"id": event.ID, "name": event.Name, "response": response,
	}), nil
}

func toolActivityMessage(reqCtx *a2asrv.ExecutorContext, partType string, data map[string]any) *a2atype.Message {
	part := a2atype.NewDataPart(data)
	part.Metadata = map[string]any{"kagent_type": partType}
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, part)
	message.TaskID, message.ContextID = reqCtx.TaskID, reqCtx.ContextID
	return message
}

func taskMessage(reqCtx *a2asrv.ExecutorContext, text string) *a2atype.Message {
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text))
	message.TaskID, message.ContextID = reqCtx.TaskID, reqCtx.ContextID
	return message
}

func (e *Executor) Cancel(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if reqCtx == nil || reqCtx.TaskID == "" || reqCtx.ContextID == "" {
			yield(nil, fmt.Errorf("task ID and context ID are required for cancellation"))
			return
		}
		e.mu.Lock()
		active := e.active
		if active == nil {
			e.mu.Unlock()
			return
		}
		if active.taskID != reqCtx.TaskID || active.contextID != reqCtx.ContextID {
			e.mu.Unlock()
			yield(nil, fmt.Errorf("cancellation does not match the active task"))
			return
		}
		active.cancel()
		done := active.done
		e.mu.Unlock()
		select {
		case <-done:
			yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil), nil)
		case <-ctx.Done():
			yield(nil, ctx.Err())
		}
	}
}

func (e *Executor) activate(task *activeTask) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active != nil {
		return false
	}
	e.active = task
	return true
}

func (e *Executor) deactivate(task *activeTask) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active == task {
		e.active = nil
	}
}

func validateRequest(reqCtx *a2asrv.ExecutorContext) (string, error) {
	if reqCtx == nil || reqCtx.Message == nil {
		return "", fmt.Errorf("A2A request message is required")
	}
	if reqCtx.TaskID == "" || reqCtx.ContextID == "" {
		return "", fmt.Errorf("task ID and context ID are required")
	}
	if reqCtx.Message.Role != a2atype.MessageRoleUser || len(reqCtx.Message.Parts) != 1 || reqCtx.Message.Parts[0] == nil {
		return "", fmt.Errorf("harness runtime accepts exactly one user text part")
	}
	text := reqCtx.Message.Parts[0].Text()
	if text == "" {
		return "", fmt.Errorf("harness runtime accepts a non-empty text part")
	}
	return text, nil
}

func safeFailure(message string) string {
	if message == "" || len(message) > 512 {
		return "Harness runtime execution failed"
	}
	return message
}

var _ runtime.EventSink = (*executionSink)(nil)
var _ a2asrv.AgentExecutor = (*Executor)(nil)
