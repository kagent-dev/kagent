package taskstore

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// A2ATaskStoreAdapter adapts KAgentTaskStore to a2asrv.TaskStore.
type A2ATaskStoreAdapter struct {
	store *KAgentTaskStore
}

// NewA2ATaskStoreAdapter creates an adapter that implements a2asrv.TaskStore
// by delegating to KAgentTaskStore.
func NewA2ATaskStoreAdapter(store *KAgentTaskStore) *A2ATaskStoreAdapter {
	return &A2ATaskStoreAdapter{store: store}
}

// Save implements a2asrv.TaskStore.
func (a *A2ATaskStoreAdapter) Save(ctx context.Context, task *a2atype.Task, _ a2atype.Event, _ a2atype.TaskVersion) (a2atype.TaskVersion, error) {
	if err := a.store.Save(ctx, task); err != nil {
		return a2atype.TaskVersionMissing, err
	}
	return a2atype.TaskVersion(1), nil
}

// Get implements a2asrv.TaskStore.
func (a *A2ATaskStoreAdapter) Get(ctx context.Context, taskID a2atype.TaskID) (*a2atype.Task, a2atype.TaskVersion, error) {
	task, err := a.store.Get(ctx, string(taskID))
	if err != nil {
		return nil, a2atype.TaskVersionMissing, err
	}
	if task == nil {
		return nil, a2atype.TaskVersionMissing, a2atype.ErrTaskNotFound
	}
	return task, a2atype.TaskVersion(1), nil
}

// List implements a2asrv.TaskStore.
// The underlying KAgentTaskStore does not support listing tasks, so this
// returns an error to signal callers that the operation is unsupported.
func (a *A2ATaskStoreAdapter) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return nil, fmt.Errorf("task listing is not supported by the KAgent task store")
}

var _ a2asrv.TaskStore = (*A2ATaskStoreAdapter)(nil)
