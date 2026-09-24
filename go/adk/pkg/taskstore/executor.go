package taskstore

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/logging"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// WrapExecutor emits a terminal/waiting event only after the native executor
// returns. That event is the TaskStore's durable handoff to pause/snapshot work;
// native cleanup must finish before the API can freeze or stop this actor.
func (s *Store) WrapExecutor(executor a2asrv.AgentExecutor) *settledExecutor {
	return &settledExecutor{AgentExecutor: executor, store: s, pending: make(map[a2a.TaskID][]*execution)}
}

type settledExecutor struct {
	a2asrv.PassthroughCallInterceptor
	a2asrv.AgentExecutor
	store   *Store
	mu      sync.Mutex
	pending map[a2a.TaskID][]*execution
}

// Before allocates per-call persistence coordination without creating tasks or
// modifying the SDK's reads. Native sessions may reserve one parked task.
func (e *settledExecutor) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if send, ok := request.Payload.(*a2a.SendMessageRequest); ok {
		// Give callers a protocol-level busy response for active work. The SDK's
		// limiter remains authoritative for simultaneous starts before tracking.
		e.mu.Lock()
		busy := len(e.pending) != 0
		e.mu.Unlock()
		if busy {
			return ctx, nil, a2a.NewError(a2a.ErrUnsupportedOperation, "instance already has active work")
		}
		if send.Message == nil {
			return ctx, nil, a2a.ErrInvalidParams
		}
		if reservation, ok := e.AgentExecutor.(interface{ ReservedTaskID() a2a.TaskID }); ok {
			if id := reservation.ReservedTaskID(); id != "" && send.Message.TaskID != id {
				return ctx, nil, a2a.NewError(a2a.ErrUnsupportedOperation, "native session is waiting for another task")
			}
		}
	}
	return context.WithValue(ctx, executionKey{}, &execution{ready: make(chan struct{})}), nil, nil
}

type executionKey struct{}
type execution struct {
	ready    chan struct{}
	failed   atomic.Bool
	canceled atomic.Bool
	boundary atomic.Int64
	cleaned  bool // guarded by settledExecutor.mu
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
		// The SDK persists events asynchronously. Establish an active task before
		// native side effects, so lifecycle operations cannot race an invisible run.
		state, ok := ctx.Value(executionKey{}).(*execution)
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
		if state.failed.Load() {
			yield(nil, sdktaskstore.ErrConcurrentModification)
			return
		}
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
	id, settleErr := e.store.instanceID()
	if settleErr == nil {
		request := &apiv1alpha1.TaskStoreServiceSettleTaskRequest{AgentInstanceId: id, TaskId: string(input.TaskID), Version: version}
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
