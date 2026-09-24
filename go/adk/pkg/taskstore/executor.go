package taskstore

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/logging"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// WrapExecutor emits a terminal/waiting event only after the native executor
// returns. That event is the TaskStore's durable handoff to pause/snapshot work;
// native cleanup must finish before the API can freeze or stop this actor.
func (s *Store) WrapExecutor(executor a2asrv.AgentExecutor) a2asrv.AgentExecutor {
	return &settledExecutor{AgentExecutor: executor, store: s, pending: make(map[a2a.TaskID][]*seed)}
}

type settledExecutor struct {
	a2asrv.AgentExecutor
	store   *Store
	mu      sync.Mutex
	pending map[a2a.TaskID][]*seed
}

func (e *settledExecutor) Execute(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	e.track(ctx, input.TaskID)
	return settledEvents(e.AgentExecutor.Execute(ctx, input))
}

func (e *settledExecutor) Cancel(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	e.track(ctx, input.TaskID)
	return settledEvents(e.AgentExecutor.Cancel(ctx, input))
}

// Execute and Cancel can overlap. The SDK runs their cleanup callbacks in
// sequence, so neither callback can wait for the other; the last one settles.
func (e *settledExecutor) track(ctx context.Context, taskID a2a.TaskID) {
	if state, ok := ctx.Value(seedKey{}).(*seed); ok {
		e.mu.Lock()
		e.pending[taskID] = append(e.pending[taskID], state)
		e.mu.Unlock()
	}
}

func (e *settledExecutor) Cleanup(ctx context.Context, input *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	if cleaner, ok := e.AgentExecutor.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, input, result, err)
	}
	state, ok := ctx.Value(seedKey{}).(*seed)
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
