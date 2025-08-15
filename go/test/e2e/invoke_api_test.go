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

func TestInvokeInlineAgent(t *testing.T) {
	// Setup
	a2aBaseURL := os.Getenv("KAGENT_API_URL")
	if a2aBaseURL == "" {
		a2aBaseURL = "http://localhost:8083/api"
	}

	// A2A URL format: <base_url>/<namespace>/<agent_name>
	agentNamespace := "kagent"

	agentName := "k8s-agent"
	a2aURL := a2aBaseURL + "/a2a/" + agentNamespace + "/" + agentName

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
	a2aBaseURL := os.Getenv("KAGENT_API_URL")
	if a2aBaseURL == "" {
		a2aBaseURL = "http://localhost:8083/api"
	}

	// A2A URL format: <base_url>/<namespace>/<agent_name>
	agentNamespace := "kagent"

	agentName := "basic-agent"
	a2aURL := a2aBaseURL + "/a2a/" + agentNamespace + "/" + agentName

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
		require.Contains(t, text, "prime", string(jsn))
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
		require.Contains(t, string(jsn), "prime", string(jsn))
	})

}
