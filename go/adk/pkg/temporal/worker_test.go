package temporal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

// newLazyTestClient creates a lazy Temporal client suitable for tests.
// It doesn't connect to a server, just validates worker registration.
func newLazyTestClient(t *testing.T) client.Client {
	t.Helper()
	c, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c
}

func TestNewWorker(t *testing.T) {
	tests := []struct {
		name       string
		useClient  bool
		taskQueue  string
		activities *Activities
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid worker creation",
			useClient:  true,
			taskQueue:  "agent-test",
			activities: &Activities{},
		},
		{
			name:       "nil client",
			useClient:  false,
			taskQueue:  "agent-test",
			activities: &Activities{},
			wantErr:    true,
			errMsg:     "temporal client must not be nil",
		},
		{
			name:       "empty task queue",
			useClient:  true,
			taskQueue:  "",
			activities: &Activities{},
			wantErr:    true,
			errMsg:     "task queue must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c client.Client
			if tt.useClient {
				c = newLazyTestClient(t)
			}

			w, err := NewWorker(c, tt.taskQueue, tt.activities)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, w)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, w)
			}
		})
	}
}

func TestNewWorkerRegistersWorkflowAndActivities(t *testing.T) {
	c := newLazyTestClient(t)
	activities := &Activities{}

	w, err := NewWorker(c, "agent-test", activities)
	require.NoError(t, err)
	require.NotNil(t, w)
	// Worker creation succeeds without panics — workflows and activities are registered.
}

func TestNewWorkerWithDifferentTaskQueues(t *testing.T) {
	c := newLazyTestClient(t)
	activities := &Activities{}

	// Create workers for different agents — each gets its own task queue.
	queues := []string{"agent-alpha", "agent-beta", "agent-gamma"}
	for _, q := range queues {
		w, err := NewWorker(c, q, activities)
		require.NoError(t, err)
		assert.NotNil(t, w)
	}
}
