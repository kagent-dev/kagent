package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestInvokeAPI(t *testing.T) {
	t.Run("when sent to the server", func(t *testing.T) {
		var a2aBaseURL string

		// Setup
		a2aBaseURL = os.Getenv("KAGENT_API_URL")
		if a2aBaseURL == "" {
			a2aBaseURL = "http://localhost:8083/api"
		}

		// A2A URL format: <base_url>/<namespace>/<agent_name>
		agentNamespace := "kagent"

		agentName := "k8s-agent"
		a2aURL := a2aBaseURL + "/a2a/" + agentNamespace + "/" + agentName

		a2aClient, err := client.NewA2AClient(a2aURL)
		require.NoError(t, err)

		t.Run("should successfully handle a synchronous agent invocation", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
				Message: protocol.Message{
					Role:  protocol.MessageRoleUser,
					Parts: []protocol.Part{protocol.NewTextPart("List all pods in the cluster")},
				},
			})
			require.NoError(t, err)

			msgResult, ok := msg.Result.(*protocol.Message)
			require.True(t, ok)
			text := a2a.ExtractText(*msgResult)
			require.Contains(t, text, "kube-scheduler-kagent-control-plane")
		})

		t.Run("should successfully handle a streaming agent invocation", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
				Message: protocol.Message{
					Role:  protocol.MessageRoleUser,
					Parts: []protocol.Part{protocol.NewTextPart("List all pods in the cluster")},
				},
			})
			require.NoError(t, err)

			var text string
			for event := range msg {
				msgResult, ok := event.Result.(*protocol.Message)
				if !ok {
					continue
				}
				text += a2a.ExtractText(*msgResult)
			}
			require.Contains(t, text, "kube-scheduler-kagent-control-plane")
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
