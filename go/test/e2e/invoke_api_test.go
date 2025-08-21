package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func a2aUrl(namespace, name string) string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		// if running locally on kind, do "kubectl port-forward -n kagent deployments/kagent-controller 8083"
		kagentURL = "http://localhost:8083"
	}
	// A2A URL format: <base_url>/<namespace>/<agent_name>
	return kagentURL + "/api/a2a/" + namespace + "/" + name
}

func TestInvokeInlineAgent(t *testing.T) {
	// Setup
	a2aURL := a2aUrl("kagent", "k8s-agent")

	a2aClient, err := client.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("sync_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all pods in the cluster")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "kube-scheduler-kagent-control-plane", string(jsn))
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all pods in the cluster")},
			},
		})
		require.NoError(t, err)

		resultList := []protocol.StreamingMessageEvent{}
		var text string
		for event := range msg {
			msgResult, ok := event.Result.(*protocol.TaskStatusUpdateEvent)
			if !ok {
				continue
			}
			if msgResult.Status.Message != nil {
				text += a2a.ExtractText(*msgResult.Status.Message)
			}
			resultList = append(resultList, event)
		}
		jsn, err := json.Marshal(resultList)
		require.NoError(t, err)
		require.Contains(t, string(jsn), "kube-scheduler-kagent-control-plane", string(jsn))
	})
}

func TestInvokeExternalAgent(t *testing.T) {
	// Setup
	a2aURL := a2aUrl("kagent", "kebab-agent")

	a2aClient, err := client.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("sync_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		// Prime numbers
		require.Contains(t, text, "kebab", string(jsn))
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		resultList := []protocol.StreamingMessageEvent{}
		var text string
		for event := range msg {
			msgResult, ok := event.Result.(*protocol.TaskStatusUpdateEvent)
			if !ok {
				continue
			}
			if msgResult.Status.Message != nil {
				text += a2a.ExtractText(*msgResult.Status.Message)
			}
			resultList = append(resultList, event)
		}
		jsn, err := json.Marshal(resultList)
		require.NoError(t, err)
		require.Contains(t, string(jsn), "kebab", string(jsn))
	})

	t.Run("invocation with different user", func(t *testing.T) {

		a2aClient, err := client.NewA2AClient(a2aURL, client.WithAPIKeyAuth("user@example.com", "x-user-id"))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		// Prime numbers
		require.Contains(t, text, "kebab", string(jsn))
	})
}
