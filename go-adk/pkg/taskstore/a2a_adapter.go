package taskstore

import (
	"context"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
)

// A2ATaskStoreAdapter implements a2asrv.TaskStore backed by KAgentTaskStore.
type A2ATaskStoreAdapter struct {
	store *KAgentTaskStore
}

// NewA2ATaskStoreAdapter creates a new A2ATaskStoreAdapter.
func NewA2ATaskStoreAdapter(store *KAgentTaskStore) *A2ATaskStoreAdapter {
	return &A2ATaskStoreAdapter{store: store}
}

// Save persists a task.
func (a *A2ATaskStoreAdapter) Save(ctx context.Context, task *a2aschema.Task) error {
	return a.store.Save(ctx, task)
}

// Get retrieves a task by ID.
func (a *A2ATaskStoreAdapter) Get(ctx context.Context, taskID a2aschema.TaskID) (*a2aschema.Task, error) {
	return a.store.Get(ctx, string(taskID))
}
