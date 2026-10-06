package taskstore

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"time"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// WrapExecutor emits a terminal/waiting event only after the native executor
// returns. Cleanup acknowledges the saved version so the API can publish it.
// The independent lifecycle worker may then pause or suspend an idle actor.
func (s *Store) WrapExecutor(executor a2asrv.AgentExecutor, runtime tracing.Runtime, flush func(context.Context) error) *settledExecutor {
	return &settledExecutor{AgentExecutor: executor, store: s, runtime: runtime, flush: flush, pending: make(map[a2a.TaskID][]*execution)}
}

type settledExecutor struct {
	a2asrv.PassthroughCallInterceptor
	a2asrv.AgentExecutor
	store   *Store
	runtime tracing.Runtime
	flush   func(context.Context) error
	mu      sync.Mutex
	pending map[a2a.TaskID][]*execution
	// admitted is a send that passed Before but has not reached Execute yet.
	admitted *execution
}

// Before allocates per-call persistence coordination without creating tasks or
// modifying the SDK's reads. Native sessions may reserve one parked task.
func (e *settledExecutor) Before(ctx context.Context, call *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	state := &execution{ready: make(chan struct{})}
	if send, ok := request.Payload.(*a2a.SendMessageRequest); ok {
		if values, ok := call.ServiceParams().Get(apia2a.DispatchHeader); ok && len(values) == 1 {
			state.dispatchID = &values[0]
		}
		if send.Message == nil {
			return ctx, nil, a2a.ErrInvalidParams
		}
		if reservation, ok := e.AgentExecutor.(interface{ ReservedTaskID() a2a.TaskID }); ok {
			if id := reservation.ReservedTaskID(); id != "" && send.Message.TaskID != id {
				return ctx, nil, a2a.NewError(a2a.ErrUnsupportedOperation, "native session is waiting for another task")
			}
		}
		// This is the only execution slot: it frees before settlement publishes the
		// boundary, so a client that saw the task finish can send immediately.
		e.mu.Lock()
		busy := len(e.pending) != 0 || e.admitted != nil
		if !busy {
			e.admitted = state
		}
		e.mu.Unlock()
		if busy {
			return ctx, nil, a2a.NewError(a2a.ErrUnsupportedOperation, "session already has active work")
		}
		// Release the slot if the call ends before the SDK starts execution.
		context.AfterFunc(ctx, func() { e.release(state) })
	}
	return context.WithValue(ctx, executionKey{}, state), nil, nil
}

func (e *settledExecutor) release(state *execution) {
	e.mu.Lock()
	if e.admitted == state {
		e.admitted = nil
		state.abandoned = true
	}
	e.mu.Unlock()
}

type executionKey struct{}
type execution struct {
	dispatchID *string
	ready      chan struct{}
	failed     atomic.Bool
	canceled   atomic.Bool
	superseded atomic.Bool // abandoned before Execute while another send held the slot
	boundary   atomic.Int64
	cleaned    bool // guarded by settledExecutor.mu
	abandoned  bool // guarded by settledExecutor.mu; its call ended before Execute
}

func executionFailed(ctx context.Context) bool {
	state, ok := ctx.Value(executionKey{}).(*execution)
	return ok && state.failed.Load()
}

func recordSaveFailure(ctx context.Context) {
	if state, ok := ctx.Value(executionKey{}).(*execution); ok {
		state.failed.Store(true)
	}
}

func recordSave(ctx context.Context, task *a2a.Task, version int64) {
	if state, ok := ctx.Value(executionKey{}).(*execution); ok {
		if task.Status.State.Terminal() || task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired {
			state.boundary.Store(version)
		} else {
			state.boundary.Store(0)
			select {
			case <-state.ready:
			default:
				close(state.ready)
			}
		}
	}
}

func (e *settledExecutor) Execute(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	e.track(ctx, input.TaskID)
	return func(yield func(a2a.Event, error) bool) {
		state, ok := ctx.Value(executionKey{}).(*execution)
		// The SDK's update manager mutates StoredTask, so copy the waiting status first.
		var waiting *a2a.TaskStatus
		if stored := input.StoredTask; stored != nil && (stored.Status.State == a2a.TaskStateInputRequired || stored.Status.State == a2a.TaskStateAuthRequired) {
			status := stored.Status
			waiting = &status
		}
		var invocation *tracing.Invocation
		if e.runtime.NativeHarness() {
			invocation = tracing.InvocationFromContext(ctx)
			if !invocation.Adopt() {
				invocation = nil
			}
			resuming := waiting != nil
			invocation.SetAttributes(tracing.RequestIdentity(input.ContextID, string(input.TaskID), resuming)...)
		}
		// The caller may disconnect while the SDK saves the initial event. Own
		// the native invocation now, and finish it if native execution never starts.
		// ADK request spans stay transport-owned; ADK traces execution separately.
		nativeStarted := false
		defer func() {
			if nativeStarted || invocation == nil {
				return
			}
			result := tracing.Result{}
			switch {
			case !ok:
				result.Error = "runtime_failure"
			case state.failed.Load():
				result.Error = "persistence_failure"
			case state.canceled.Load():
				result = tracing.Result{TaskState: string(a2a.TaskStateCanceled), Disposition: tracing.DispositionCanceled}
			case ctx.Err() != nil:
				result.Disposition = tracing.DispositionInterrupted
			default:
				result.Disposition = tracing.DispositionAbandoned
			}
			if _, err := invocation.End(ctx, result); err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "export runtime invocation traces", "task_id", input.TaskID, "error", err)
			}
		}()
		// The SDK persists events asynchronously. Establish an active task before
		// native side effects, so lifecycle operations cannot race an invisible run.
		if !ok {
			yield(nil, fmt.Errorf("runtime execution interceptor is required"))
			return
		}
		var initial a2a.Event = a2a.NewStatusUpdateEvent(input, a2a.TaskStateWorking, nil)
		if input.StoredTask == nil {
			initial = &a2a.Task{ID: input.TaskID, ContextID: input.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}, History: []*a2a.Message{input.Message}}
		}
		if !yield(initial, nil) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-state.ready:
		}
		if state.canceled.Load() {
			return
		}
		if state.superseded.Load() {
			// No CancelTask supplies a boundary here: cancel a new task, or return a
			// reply's task to its waiting state rather than cancel work the user parked.
			if waiting != nil {
				yield(a2a.NewStatusUpdateEvent(input, waiting.State, waiting.Message), nil)
			} else {
				yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateCanceled, nil), nil)
			}
			return
		}
		if state.failed.Load() {
			yield(nil, sdktaskstore.ErrConcurrentModification)
			return
		}
		if ctx.Err() != nil {
			return
		}
		nativeStarted = true
		for event, err := range settledEvents(e.AgentExecutor.Execute(ctx, input)) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (e *settledExecutor) Cancel(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	// A cancellation event may wait behind the first save. Stop native startup
	// immediately, before the SDK consumes that event.
	e.mu.Lock()
	for _, call := range e.pending[input.TaskID] {
		call.canceled.Store(true)
	}
	e.mu.Unlock()
	e.track(ctx, input.TaskID)
	return settledEvents(e.AgentExecutor.Cancel(ctx, input))
}

// Execute and Cancel can overlap. The SDK runs their cleanup callbacks in
// sequence, so neither callback can wait for the other; the last one settles.
func (e *settledExecutor) track(ctx context.Context, taskID a2a.TaskID) {
	if state, ok := ctx.Value(executionKey{}).(*execution); ok {
		e.mu.Lock()
		if e.admitted == state {
			e.admitted = nil
		} else if state.abandoned && (e.admitted != nil || len(e.pending) != 0) {
			// The SDK detaches execution, so a disconnected send can arrive after its
			// slot went to another send. Cancel it before native work starts.
			state.superseded.Store(true)
		}
		e.pending[taskID] = append(e.pending[taskID], state)
		e.mu.Unlock()
	}
}

func (e *settledExecutor) Cleanup(ctx context.Context, input *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	if cleaner, ok := e.AgentExecutor.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, input, result, err)
	}
	state, ok := ctx.Value(executionKey{}).(*execution)
	if !ok {
		return
	}
	e.mu.Lock()
	state.cleaned = true
	version := state.boundary.Load()
	for _, call := range e.pending[input.TaskID] {
		if !call.cleaned {
			e.mu.Unlock()
			return
		}
		version = max(version, call.boundary.Load())
	}
	delete(e.pending, input.TaskID)
	e.mu.Unlock()
	if version == 0 {
		return
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	// Export the final save before settlement permits suspension. The settlement
	// RPC's own span is flushed afterwards, but suspension can race that flush.
	if e.flush != nil {
		if err := e.flush(finish); err != nil {
			logging.FromContext(finish).ErrorContext(finish, "flush telemetry before task settlement", "task_id", input.TaskID, "error", err)
		}
		defer func() {
			if err := e.flush(finish); err != nil {
				logging.FromContext(finish).ErrorContext(finish, "flush telemetry after task settlement", "task_id", input.TaskID, "error", err)
			}
		}()
	}
	id, settleErr := e.store.sessionID()
	if settleErr == nil {
		request := &apiv1alpha1.TaskStoreServiceSettleTaskRequest{SessionId: id, TaskId: string(input.TaskID), Version: version}
		settleErr = e.store.retry(finish, func(ctx context.Context) error {
			_, err := e.store.client.TaskStoreService().SettleTask(ctx, request)
			return err
		})
	}
	if settleErr != nil {
		logging.FromContext(finish).ErrorContext(finish, "settle runtime task boundary", "task_id", input.TaskID, "error", settleErr)
	}
}

func settledEvents(events iter.Seq2[a2a.Event, error]) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var boundary a2a.Event
		for event, err := range events {
			if err != nil {
				yield(nil, err)
				return
			}
			if boundary != nil {
				yield(nil, fmt.Errorf("runtime emitted an event after its final boundary"))
				return
			}
			var state a2a.TaskState
			switch value := event.(type) {
			case *a2a.Task:
				state = value.Status.State
			case *a2a.TaskStatusUpdateEvent:
				state = value.Status.State
			}
			if state.Terminal() || state == a2a.TaskStateInputRequired || state == a2a.TaskStateAuthRequired {
				boundary = event
			} else if !yield(event, nil) {
				return
			}
		}
		if boundary != nil {
			yield(boundary, nil)
		}
	}
}
