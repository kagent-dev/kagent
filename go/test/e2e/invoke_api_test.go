package e2e_test

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/test/mockllm"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

//go:embed mocks
var mocks embed.FS

func a2aUrl(namespace, name string) string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		// if running locally on kind, do "kubectl port-forward -n kagent deployments/kagent-controller 8083"
		kagentURL = "http://localhost:8083"
	}
	// A2A URL format: <base_url>/<namespace>/<agent_name>
	return kagentURL + "/api/a2a/" + namespace + "/" + name
}

func generateModelCfg(baseURL string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-config",
			Namespace: "kagent",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4.1-mini",
			APIKeySecret:    "kagent-openai",
			APIKeySecretKey: "OPENAI_API_KEY",
			Provider:        v1alpha2.ModelProviderOpenAI,
			OpenAI: &v1alpha2.OpenAIConfig{
				BaseURL: baseURL,
			},
		},
	}
}

func generateAgent() *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "kagent",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ModelConfig:   "test-model-config",
				SystemMessage: "You are a test agent. The system prompt doesn't matter because we're using a mock server.",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								ApiGroup: "kagent.dev",
								Kind:     "RemoteMCPServer",
								Name:     "kagent-tool-server",
							},
							ToolNames: []string{"k8s_get_resources"},
						},
					},
				},
			},
		},
	}
}

func buildK8sURL(baseURL string) string {
	// Get the port from the listener address
	splitted := strings.Split(baseURL, ":")
	port := splitted[len(splitted)-1]
	// Check local OS and use the correct local host
	var localHost string
	switch runtime.GOOS {
	case "darwin":
		localHost = "host.docker.internal"
	case "linux":
		localHost = "172.17.0.1"
	}

	if os.Getenv("KAGENT_LOCAL_HOST") != "" {
		localHost = os.Getenv("KAGENT_LOCAL_HOST")
	}

	return fmt.Sprintf("http://%s:%s", localHost, port)

}

func TestInvokeInlineAgent(t *testing.T) {

	mockllmCfg, err := mockllm.LoadConfigFromFile("mocks/invoke_inline_agent.json", mocks)
	require.NoError(t, err)

	server := mockllm.NewServer(mockllmCfg)
	baseURL, err := server.Start()
	baseURL = buildK8sURL(baseURL)
	require.NoError(t, err)
	defer server.Stop() //nolint:errcheck

	cfg, err := config.GetConfig()
	require.NoError(t, err)

	scheme := k8s_runtime.NewScheme()
	err = v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	cli, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	modelCfg := generateModelCfg(baseURL + "/v1")
	err = cli.Create(t.Context(), modelCfg)
	require.NoError(t, err)
	agent := generateAgent()
	err = cli.Create(t.Context(), agent)
	require.NoError(t, err)

	defer func() {
		cli.Delete(t.Context(), modelCfg) //nolint:errcheck
		cli.Delete(t.Context(), agent)    //nolint:errcheck
	}()

	args := []string{
		"wait",
		"--for",
		"condition=Ready",
		"--timeout=1m",
		"agents.kagent.dev",
		"test-agent",
		"-n",
		"kagent",
	}

	cmd := exec.CommandContext(t.Context(), "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// Setup
	a2aURL := a2aUrl("kagent", "test-agent")

	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("sync_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all nodes in the cluster")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "kagent-control-plane", string(jsn))
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all nodes in the cluster")},
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
		require.Contains(t, string(jsn), "kagent-control-plane", string(jsn))
	})
}

func TestInvokeExternalAgent(t *testing.T) {
	// Setup
	a2aURL := a2aUrl("kagent", "kebab-agent")

	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
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

		a2aClient, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithAPIKeyAuth("user@example.com", "x-user-id"))
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
		require.Contains(t, text, "kebab for user@example.com", string(jsn))
	})
}
