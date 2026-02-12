package taskstore

import (
	"context"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
)

// A2APushConfigAdapter implements a2asrv.PushConfigStore backed by KAgentPushNotificationStore.
type A2APushConfigAdapter struct {
	store *KAgentPushNotificationStore
}

// NewA2APushConfigAdapter creates a new A2APushConfigAdapter.
func NewA2APushConfigAdapter(store *KAgentPushNotificationStore) *A2APushConfigAdapter {
	return &A2APushConfigAdapter{store: store}
}

// Save stores a push notification configuration for a task.
func (a *A2APushConfigAdapter) Save(ctx context.Context, taskID a2aschema.TaskID, config *a2aschema.PushConfig) (*a2aschema.PushConfig, error) {
	return a.store.Save(ctx, string(taskID), config)
}

// Get retrieves a push notification configuration.
func (a *A2APushConfigAdapter) Get(ctx context.Context, taskID a2aschema.TaskID, configID string) (*a2aschema.PushConfig, error) {
	return a.store.Get(ctx, string(taskID), configID)
}

// List retrieves all push notification configurations for a task.
func (a *A2APushConfigAdapter) List(ctx context.Context, taskID a2aschema.TaskID) ([]*a2aschema.PushConfig, error) {
	return a.store.List(ctx, string(taskID))
}

// Delete removes a push notification configuration.
func (a *A2APushConfigAdapter) Delete(ctx context.Context, taskID a2aschema.TaskID, configID string) error {
	return a.store.Delete(ctx, string(taskID), configID)
}

// DeleteAll removes all push notification configurations for a task.
func (a *A2APushConfigAdapter) DeleteAll(ctx context.Context, taskID a2aschema.TaskID) error {
	return a.store.DeleteAll(ctx, string(taskID))
}
