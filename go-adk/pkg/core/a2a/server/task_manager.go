package server

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/taskstore"
)

// KAgentExecutor implements a2asrv.AgentExecutor by bridging to core.A2aAgentExecutor.
type KAgentExecutor struct {
	executor *core.A2aAgentExecutor
}

// NewKAgentExecutor creates a new KAgentExecutor.
func NewKAgentExecutor(executor *core.A2aAgentExecutor) *KAgentExecutor {
	return &KAgentExecutor{executor: executor}
}

// Execute implements a2asrv.AgentExecutor.
func (e *KAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	// Bridge the eventqueue.Queue to our a2a.EventQueue interface
	bridgeQueue := &eventQueueBridge{queue: queue}

	// Build MessageSendParams from the request context
	params := &a2atype.MessageSendParams{
		Message:  reqCtx.Message,
		Metadata: reqCtx.Metadata,
	}

	taskID := string(reqCtx.TaskID)
	contextID := reqCtx.ContextID

	return e.executor.Execute(ctx, params, bridgeQueue, taskID, contextID)
}

// Cancel implements a2asrv.AgentExecutor.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	// Write a canceled status event
	event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// eventQueueBridge adapts eventqueue.Queue (a2asrv) to a2a.EventQueue (our interface).
type eventQueueBridge struct {
	queue eventqueue.Queue
}

// EnqueueEvent implements a2a.EventQueue by delegating to eventqueue.Queue.Write.
func (b *eventQueueBridge) EnqueueEvent(ctx context.Context, event a2atype.Event) error {
	if err := b.queue.Write(ctx, event); err != nil {
		return fmt.Errorf("failed to write event to queue: %w", err)
	}
	return nil
}

// Compile-time check
var _ a2a.EventQueue = (*eventQueueBridge)(nil)
var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// KAgentTaskStoreAdapter adapts taskstore.KAgentTaskStore to a2asrv.TaskStore.
type KAgentTaskStoreAdapter struct {
	store *taskstore.KAgentTaskStore
}

// NewKAgentTaskStoreAdapter creates an adapter for a2asrv.
func NewKAgentTaskStoreAdapter(store *taskstore.KAgentTaskStore) *KAgentTaskStoreAdapter {
	return &KAgentTaskStoreAdapter{store: store}
}

// Save implements a2asrv.TaskStore.
func (a *KAgentTaskStoreAdapter) Save(ctx context.Context, task *a2atype.Task, _ a2atype.Event, _ a2atype.TaskVersion) (a2atype.TaskVersion, error) {
	if err := a.store.Save(ctx, task); err != nil {
		return a2atype.TaskVersionMissing, err
	}
	return a2atype.TaskVersion(1), nil
}

// Get implements a2asrv.TaskStore.
func (a *KAgentTaskStoreAdapter) Get(ctx context.Context, taskID a2atype.TaskID) (*a2atype.Task, a2atype.TaskVersion, error) {
	task, err := a.store.Get(ctx, string(taskID))
	if err != nil {
		return nil, a2atype.TaskVersionMissing, err
	}
	if task == nil {
		return nil, a2atype.TaskVersionMissing, a2atype.ErrTaskNotFound
	}
	return task, a2atype.TaskVersion(1), nil
}

// List implements a2asrv.TaskStore. KAgent backend does not expose a list API; returns empty.
func (a *KAgentTaskStoreAdapter) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	return &a2atype.ListTasksResponse{
		Tasks:         []*a2atype.Task{},
		TotalSize:     0,
		PageSize:      pageSize,
		NextPageToken: "",
	}, nil
}

var _ a2asrv.TaskStore = (*KAgentTaskStoreAdapter)(nil)
