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

	mu sync.Mutex
	// state serializes access to the Actor's one native conversation. It also
	// identifies the component that currently owns native process cleanup:
	// nil -> active -> parked -> active, or parked -> canceling -> nil.
	state executorState
}

type executorState interface{ isExecutorState() }

type taskRef struct {
	taskID    a2atype.TaskID
	contextID string
}

type activeTask struct {
	taskRef
	// Execute closes done after Run or Resume has relinquished the native turn.
	// Cancel sets cancelRequested before interrupting the execution context.
	cancel          context.CancelFunc
	done            chan struct{}
	cancelRequested bool
}

// parkedTask owns the PendingTurn while its A2A task is waiting for input.
type parkedTask struct {
	taskRef
	pending runtime.PendingTurn
}

// cancelingTask keeps the Actor occupied while PendingTurn.Cancel performs
// native RPC and process cleanup outside the executor mutex.
type cancelingTask struct {
	taskRef
	done chan struct{}
	err  error
}

func (*activeTask) isExecutorState()    {}
func (*parkedTask) isExecutorState()    {}
func (*cancelingTask) isExecutorState() {}

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
		turn, err := validateRequest(reqCtx)
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
		active := &activeTask{
			taskRef: taskRef{taskID: reqCtx.TaskID, contextID: reqCtx.ContextID},
			cancel:  cancel,
			done:    make(chan struct{}),
		}
		resuming := reqCtx.StoredTask != nil && requiresInput(reqCtx.StoredTask.Status.State)
		pending, err := e.activate(active, resuming)
		if err != nil {
			cancel()
			yield(nil, err)
			return
		}
		var finishOnce sync.Once
		finishedCanceled := false
		// Exactly one exit path either releases the Actor or parks the native
		// handle. The deferred finish covers every early return.
		finish := func() bool {
			finishOnce.Do(func() {
				cancel()
				finishedCanceled = e.deactivate(active)
				close(active.done)
			})
			return finishedCanceled
		}
		park := func(pending runtime.PendingTurn) bool {
			parked := false
			finishOnce.Do(func() {
				cancel()
				parked = e.park(active, pending)
				close(active.done)
			})
			return parked
		}
		defer finish()

		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}
		sink := &executionSink{reqCtx: reqCtx, yield: yield, continuation: e.continuation}
		turn.ContinuationID = continuationID
		var outcome runtime.Outcome
		var runErr error
		// activate returns a handle only when this request continues the task
		// currently waiting for input. New turns enter through Runner.Run.
		if pending == nil {
			outcome, runErr = e.runner.Run(runCtx, turn, sink)
		} else {
			outcome, runErr = pending.Resume(runCtx, turn.InputResponse, sink)
		}
		if errors.Is(runErr, errYieldStopped) {
			return
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return
		}
		if runErr != nil {
			a2alog.Error(ctx, "Harness runtime execution failed", runErr)
			if finish() {
				return
			}
			message := taskMessage(reqCtx, "Harness runtime execution failed")
			message.SetMeta(apia2a.TimelinePositionMetadataKey, sink.nextTimelinePosition())
			yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, message), nil)
			return
		}
		if outcome.Failure != nil && outcome.Pending != nil {
			_ = outcome.Pending.Cancel(context.Background())
			if finish() {
				return
			}
			yield(nil, fmt.Errorf("runtime returned both a failure and a pending turn"))
			return
		}

		if outcome.Pending != nil {
			message, err := inputRequiredMessage(reqCtx, outcome.Pending.Request())
			if err != nil {
				_ = outcome.Pending.Cancel(context.Background())
				yield(nil, err)
				return
			}
			// Transfer the live native turn into executor state before telling the
			// caller that it can submit a response or cancellation.
			if !park(outcome.Pending) {
				_ = outcome.Pending.Cancel(context.Background())
				return
			}
			message.SetMeta(apia2a.TimelinePositionMetadataKey, sink.nextTimelinePosition())
			yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateInputRequired, message), nil)
			return
		}
		if finish() {
			return
		}
		// Reap the runtime process and release the Actor's active-task slot before
		// publishing a terminal state. A client may submit its next turn as soon
		// as it observes this event.
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

func inputRequiredMessage(reqCtx *a2asrv.ExecutorContext, request runtime.InputRequest) (*a2atype.Message, error) {
	switch request := request.(type) {
	case *runtime.ApprovalRequest:
		if request.ID == "" || request.CallID == "" || request.Name == "" {
			return nil, fmt.Errorf("runtime tool approval request is incomplete")
		}
		message := taskMessage(reqCtx, request.Hint)
		if err := apia2a.AttachHITL(message, apia2a.ToolApprovalRequest{
			Type:  apia2a.HITLTypeToolApprovalRequest,
			Tools: []apia2a.HITLTool{{ID: request.ID, CallID: request.CallID, Name: request.Name, Args: request.Args}},
		}); err != nil {
			return nil, err
		}
		return message, nil
	case *runtime.AskUserRequest:
		if request.ID == "" || len(request.Questions) == 0 {
			return nil, fmt.Errorf("runtime ask-user request is incomplete")
		}
		questions := make([]apia2a.HITLQuestion, 0, len(request.Questions))
		for _, question := range request.Questions {
			if question.Question == "" {
				return nil, fmt.Errorf("runtime ask-user request contains an empty question")
			}
			questions = append(questions, apia2a.HITLQuestion{
				Question: question.Question,
				Choices:  append([]string(nil), question.Choices...),
				Multiple: question.Multiple,
			})
		}
		message := taskMessage(reqCtx, request.Hint)
		if err := apia2a.AttachHITL(message, apia2a.AskUserRequest{
			Type: apia2a.HITLTypeAskUserRequest, ID: request.ID, Questions: questions,
		}); err != nil {
			return nil, err
		}
		return message, nil
	default:
		return nil, fmt.Errorf("runtime returned unsupported input request %T", request)
	}
}

// Cancel stops the matching active or parked task and waits for its Runner to exit.
func (e *Executor) Cancel(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if reqCtx == nil || reqCtx.TaskID == "" || reqCtx.ContextID == "" {
			yield(nil, fmt.Errorf("task ID and context ID are required for cancellation"))
			return
		}
		ref := taskRef{taskID: reqCtx.TaskID, contextID: reqCtx.ContextID}
		e.mu.Lock()
		switch state := e.state.(type) {
		case *activeTask:
			if !state.matches(ref) {
				e.mu.Unlock()
				yield(nil, fmt.Errorf("cancellation does not match the active task"))
				return
			}
			state.cancelRequested = true
			state.cancel()
			done := state.done
			e.mu.Unlock()
			yieldCancellation(ctx, reqCtx, done, nil, yield)
			return
		case *parkedTask:
			if !state.matches(ref) {
				e.mu.Unlock()
				yield(nil, fmt.Errorf("cancellation does not match the parked task"))
				return
			}
			canceling := &cancelingTask{taskRef: ref, done: make(chan struct{})}
			e.state = canceling
			e.mu.Unlock()

			err := state.pending.Cancel(ctx)
			e.mu.Lock()
			canceling.err = err
			if e.state == canceling {
				e.state = nil
			}
			close(canceling.done)
			e.mu.Unlock()
			yieldCancellation(ctx, reqCtx, canceling.done, err, yield)
			return
		case *cancelingTask:
			if !state.matches(ref) {
				e.mu.Unlock()
				yield(nil, fmt.Errorf("cancellation does not match the task being canceled"))
				return
			}
			done := state.done
			e.mu.Unlock()
			select {
			case <-done:
				yieldCancellation(ctx, reqCtx, done, state.err, yield)
			case <-ctx.Done():
				yield(nil, ctx.Err())
			}
			return
		case nil:
			e.mu.Unlock()
			return
		default:
			e.mu.Unlock()
			yield(nil, fmt.Errorf("runtime actor has an invalid task state"))
		}
	}
}

func yieldCancellation(ctx context.Context, reqCtx *a2asrv.ExecutorContext, done <-chan struct{}, knownErr error, yield func(a2atype.Event, error) bool) {
	select {
	case <-done:
		if knownErr != nil {
			yield(nil, knownErr)
			return
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil), nil)
	case <-ctx.Done():
		yield(nil, ctx.Err())
	}
}

func (r taskRef) matches(other taskRef) bool {
	return r.taskID == other.taskID && r.contextID == other.contextID
}

func (e *Executor) activate(task *activeTask, resuming bool) (runtime.PendingTurn, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch state := e.state.(type) {
	case nil:
		if resuming {
			return nil, fmt.Errorf("runtime has no parked turn to continue")
		}
		e.state = task
		return nil, nil
	case *parkedTask:
		if !resuming {
			return nil, errBusy
		}
		if !state.matches(task.taskRef) {
			return nil, fmt.Errorf("continuation does not match the parked task")
		}
		e.state = task
		return state.pending, nil
	default:
		return nil, errBusy
	}
}

func (e *Executor) park(task *activeTask, pending runtime.PendingTurn) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != task {
		return false
	}
	if task.cancelRequested {
		// Cancellation won while the runtime was producing its input request.
		// The caller must cancel the newly returned handle instead of parking it.
		e.state = nil
		return false
	}
	e.state = &parkedTask{taskRef: task.taskRef, pending: pending}
	return true
}

func (e *Executor) deactivate(task *activeTask) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == task {
		e.state = nil
	}
	return task.cancelRequested
}

func validateRequest(reqCtx *a2asrv.ExecutorContext) (runtime.Turn, error) {
	if reqCtx == nil || reqCtx.Message == nil {
		return runtime.Turn{}, fmt.Errorf("A2A request message is required")
	}
	if reqCtx.TaskID == "" || reqCtx.ContextID == "" {
		return runtime.Turn{}, fmt.Errorf("task ID and context ID are required")
	}
	if reqCtx.Message.Role != a2atype.MessageRoleUser {
		return runtime.Turn{}, fmt.Errorf("harness runtime accepts only user messages")
	}
	if reqCtx.StoredTask != nil && requiresInput(reqCtx.StoredTask.Status.State) {
		approvalRequest, err := apia2a.ParseToolApprovalRequest(reqCtx.StoredTask.Status.Message)
		if err != nil {
			return runtime.Turn{}, err
		}
		if approvalRequest != nil {
			response, err := apia2a.ParseToolApprovalResponse(reqCtx.Message)
			if err != nil {
				return runtime.Turn{}, err
			}
			if err := apia2a.ValidateToolApprovalResponse(approvalRequest, response); err != nil {
				return runtime.Turn{}, err
			}
			decision := response.Approvals[0]
			return runtime.Turn{InputResponse: &runtime.ApprovalDecision{
				ID: decision.ID, Approved: decision.Approved, RejectionReason: decision.RejectionReason,
			}}, nil
		}
		askRequest, err := apia2a.ParseAskUserRequest(reqCtx.StoredTask.Status.Message)
		if err != nil {
			return runtime.Turn{}, err
		}
		if askRequest == nil {
			return runtime.Turn{}, fmt.Errorf("stored input-required task has no supported request")
		}
		response, err := apia2a.ParseAskUserResponse(reqCtx.Message)
		if err != nil {
			return runtime.Turn{}, err
		}
		if err := apia2a.ValidateAskUserResponse(askRequest, response); err != nil {
			return runtime.Turn{}, err
		}
		answers := make([][]string, len(response.Answers))
		for index, answer := range response.Answers {
			answers[index] = append([]string(nil), answer.Answer...)
		}
		return runtime.Turn{InputResponse: &runtime.AskUserResponse{ID: response.ID, Answers: answers}}, nil
	}
	if len(reqCtx.Message.Parts) != 1 || reqCtx.Message.Parts[0] == nil {
		return runtime.Turn{}, fmt.Errorf("harness runtime accepts exactly one user text part")
	}
	text := reqCtx.Message.Parts[0].Text()
	if text == "" {
		return runtime.Turn{}, fmt.Errorf("harness runtime accepts a non-empty text part")
	}
	return runtime.Turn{Prompt: text}, nil
}

func requiresInput(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func safeFailure(message string) string {
	if message == "" || len(message) > 512 {
		return "Harness runtime execution failed"
	}
	return message
}

var _ runtime.EventSink = (*executionSink)(nil)
var _ a2asrv.AgentExecutor = (*Executor)(nil)
