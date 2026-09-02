// Package a2a supervises Harness runtime turns behind kagent's private A2A
// service. Public Task persistence remains owned by the controller gateway.
package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2alog "github.com/a2aproject/a2a-go/v2/log"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
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

// Executor maps one native Harness conversation onto private A2A execution.
// Each Actor accepts only one active task so ordered native continuation and
// cancellation semantics remain unambiguous.
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
	reqCtx         *a2asrv.ExecutorContext
	yield          func(a2atype.Event, error) bool
	continuation   ContinuationStore
	textArtifactID a2atype.ArtifactID
	lastPosition   time.Time
}

var (
	errBusy         = errors.New("runtime actor already has an active task")
	errYieldStopped = errors.New("A2A event consumer stopped")
)

// New constructs the shared executor used by native Harness implementations.
func New(runner Runner, continuation ContinuationStore) (*Executor, error) {
	if runner == nil || continuation == nil {
		return nil, fmt.Errorf("runner and continuation store are required")
	}
	return &Executor{runner: runner, continuation: continuation}, nil
}

// Execute validates and serializes one A2A request onto the native Runner.
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
		sink := &executionSink{reqCtx: reqCtx, yield: yield, continuation: e.continuation}
		outcome, runErr := e.runner.Run(runCtx, runtime.Turn{
			Prompt: prompt, ContinuationID: continuationID,
		}, sink)
		if errors.Is(runErr, errYieldStopped) {
			return
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return
		}
		if runErr != nil {
			a2alog.Error(ctx, "Harness runtime execution failed", runErr)
			finish()
			message := taskMessage(reqCtx, "Harness runtime execution failed")
			message.SetMeta(apia2a.TimelinePositionMetadataKey, sink.nextTimelinePosition())
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
		message.SetMeta(apia2a.TimelinePositionMetadataKey, sink.nextTimelinePosition())
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
	if s.textArtifactID == "" {
		update = a2atype.NewArtifactEvent(s.reqCtx, a2atype.NewTextPart(event.Text))
		s.textArtifactID = update.Artifact.ID
		update.Artifact.SetMeta(apia2a.TimelinePositionMetadataKey, s.nextTimelinePosition())
	} else {
		update = a2atype.NewArtifactUpdateEvent(s.reqCtx, s.textArtifactID, a2atype.NewTextPart(event.Text))
	}
	if !s.yield(update, nil) {
		return errYieldStopped
	}
	return nil
}

func (s *executionSink) ToolCall(event runtime.ToolCall) error {
	part, err := toolCallPart(event)
	if err != nil {
		return err
	}
	return s.emitToolArtifact(part)
}

func (s *executionSink) ToolResult(event runtime.ToolResult) error {
	part, err := toolResultPart(event)
	if err != nil {
		return err
	}
	return s.emitToolArtifact(part)
}

func (s *executionSink) emitToolArtifact(part *a2atype.Part) error {
	// Append relates deltas within one contiguous text run. Tool activity closes
	// that run and is an agent-produced artifact of its own, matching the Go ADK's
	// OutputArtifactPerEvent representation.
	s.textArtifactID = ""
	update := a2atype.NewArtifactEvent(s.reqCtx, part)
	update.LastChunk = true
	update.Artifact.SetMeta(apia2a.TimelinePositionMetadataKey, s.nextTimelinePosition())
	if !s.yield(update, nil) {
		return errYieldStopped
	}
	return nil
}

func (s *executionSink) nextTimelinePosition() string {
	position := time.Now().UTC()
	if !position.After(s.lastPosition) {
		position = s.lastPosition.Add(time.Nanosecond)
	}
	s.lastPosition = position
	return position.Format(time.RFC3339Nano)
}

func toolCallPart(event runtime.ToolCall) (*a2atype.Part, error) {
	if event.ID == "" || event.Name == "" {
		return nil, fmt.Errorf("runtime tool call requires an ID and name")
	}
	args := event.Arguments
	if args == nil {
		args = map[string]any{}
	}
	return toolActivityPart("function_call", map[string]any{
		"id": event.ID, "name": event.Name, "args": args,
	}), nil
}

func toolResultPart(event runtime.ToolResult) (*a2atype.Part, error) {
	if event.ID == "" || event.Name == "" {
		return nil, fmt.Errorf("runtime tool result requires an ID and name")
	}
	response := map[string]any{"result": event.Result}
	if event.IsError {
		response["isError"] = true
	}
	return toolActivityPart("function_response", map[string]any{
		"id": event.ID, "name": event.Name, "response": response,
	}), nil
}

func toolActivityPart(partType string, data map[string]any) *a2atype.Part {
	part := a2atype.NewDataPart(data)
	part.Metadata = map[string]any{"kagent_type": partType}
	return part
}

func taskMessage(reqCtx *a2asrv.ExecutorContext, text string) *a2atype.Message {
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text))
	message.TaskID, message.ContextID = reqCtx.TaskID, reqCtx.ContextID
	return message
}

// Cancel stops the matching active task and waits for its Runner to exit.
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
