package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestInvokeAPI(t *testing.T) {
	t.Run("when sent to the server", func(t *testing.T) {
		var a2aBaseURL string

		// Setup
		a2aBaseURL = os.Getenv("KAGENT_A2A_URL")
		if a2aBaseURL == "" {
			a2aBaseURL = "http://localhost:8083/api/a2a"
		}

		// A2A URL format: <base_url>/<namespace>/<agent_name>
		agentNamespace := "kagent"

		agentName := "k8s-agent"
		a2aURL := a2aBaseURL + "/" + agentNamespace + "/" + agentName

		a2aClient, err := client.NewA2AClient(a2aURL)
		require.NoError(t, err)

		t.Run("should successfully handle an agent invocation", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Send task using A2A protocol
			task, err := a2aClient.SendTasks(ctx, protocol.SendTaskParams{
				ID:        "kagent-test-task-" + uuid.NewString(),
				SessionID: nil, // New session
				Message: protocol.Message{
					Role:  protocol.MessageRoleUser,
					Parts: []protocol.Part{protocol.NewTextPart("List all pods in the cluster")},
				},
			})
			require.NoError(t, err)

			require.NotEmpty(t, task.ID)
			assert.Contains(t, []protocol.TaskState{
				protocol.TaskStateSubmitted,
				protocol.TaskStateWorking,
				protocol.TaskStateCompleted,
			}, task.Status.State)

			// Wait for task completion
			finalTask, err := waitForTaskCompletion(ctx, a2aClient, task.ID, 30*time.Second)
			require.NoError(t, err)

			assert.Equal(t, protocol.TaskStateCompleted, finalTask.Status.State)
			assert.NotEmpty(t, finalTask.Artifacts)
			assert.NotEmpty(t, finalTask.Artifacts[0].Parts)
			assert.NotEmpty(t, finalTask.Artifacts[0].Parts[0])
			textPart, ok := finalTask.Artifacts[0].Parts[0].(protocol.TextPart)
			assert.True(t, ok)
			assert.Contains(t, textPart.Text, "kube-scheduler-kagent-control-plane")
		})
	})
}

// waitForTaskCompletion polls the task until it's completed or times out
func waitForTaskCompletion(ctx context.Context, a2aClient *client.A2AClient, taskID string, timeout time.Duration) (*protocol.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			task, err := a2aClient.GetTasks(ctx, protocol.TaskQueryParams{
				ID: taskID,
			})
			if err != nil {
				return nil, err
			}

			switch task.Status.State {
			case protocol.TaskStateSubmitted,
				protocol.TaskStateWorking:
				continue // Keep polling
			case protocol.TaskStateCompleted,
				protocol.TaskStateFailed,
				protocol.TaskStateCanceled:
				return task, nil
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
