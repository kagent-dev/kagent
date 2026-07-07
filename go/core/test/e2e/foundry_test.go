package e2e_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	foundryChatDeployment      = "gpt-4-1-nano"
	foundryEmbeddingDeployment = "text-embedding-3-small-memory"
	foundryAPIVersion          = "2024-10-21"
)

// setupFoundryModelConfig creates a Foundry ModelConfig pointing at the mock
// endpoint. Authentication is implicit: because apiKeySecret is set the runtime
// uses the API key (mounted as FOUNDRY_API_KEY); the mock does not validate it.
func setupFoundryModelConfig(t *testing.T, cli client.Client, endpoint, deployment, model string) *v1alpha2.ModelConfig {
	modelCfg := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-foundry-model-config-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           model,
			Provider:        v1alpha2.ModelProviderFoundry,
			APIKeySecret:    "kagent-openai",
			APIKeySecretKey: "OPENAI_API_KEY",
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   endpoint,
				Deployment: deployment,
				APIVersion: foundryAPIVersion,
			},
		},
	}
	require.NoError(t, cli.Create(t.Context(), modelCfg))
	cleanup(t, cli, modelCfg)
	return modelCfg
}

// setupFoundryMemoryMockServer stands up a mock Azure AI Foundry data plane that
// serves both the chat-completions deployment and the embeddings deployment on
// their OpenAI-compatible paths, and returns a cluster-reachable URL.
func setupFoundryMemoryMockServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openai/deployments/" + foundryChatDeployment + "/chat/completions":
			require.Equal(t, foundryAPIVersion, r.URL.Query().Get("api-version"))
			writeFoundryMemoryChatResponse(t, w, r)
		case "/openai/deployments/" + foundryEmbeddingDeployment + "/embeddings":
			require.Equal(t, foundryAPIVersion, r.URL.Query().Get("api-version"))
			var body struct {
				Input      []string `json:"input"`
				Dimensions int      `json:"dimensions"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.NotEmpty(t, body.Input)
			require.Equal(t, 768, body.Dimensions)
			writeFoundryEmbeddingResponse(w)
		default:
			http.Error(w, fmt.Sprintf("unexpected Foundry mock path %s", r.URL.Path), http.StatusNotFound)
		}
	}))
	server.Listener = listener
	server.Start()

	clusterURL := buildK8sURL(server.URL)
	return clusterURL, server.Close
}

func writeFoundryMemoryChatResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var body struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

	for _, message := range slices.Backward(body.Messages) {
		switch {
		case message.Role == "tool" && strings.Contains(message.Content, "Successfully saved information to long-term memory"):
			fmt.Fprint(w, `{"id":"chatcmpl-foundry-mem-2","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"I have saved your preferences to memory: you prefer dark mode and Go over Python."},"finish_reason":"stop"}]}`)
			return
		case message.Role == "tool" && strings.Contains(message.Content, "dark mode"):
			fmt.Fprint(w, `{"id":"chatcmpl-foundry-mem-4","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"Based on your memory, you prefer dark mode and Go over Python."},"finish_reason":"stop"}]}`)
			return
		case message.Role == "user" && strings.Contains(message.Content, "Remember that I prefer dark mode and Go over Python"):
			fmt.Fprint(w, `{"id":"chatcmpl-foundry-mem-1","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_save_1","type":"function","function":{"name":"save_memory","arguments":"{\"content\": \"User prefers dark mode and Go over Python\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		case message.Role == "user" && strings.Contains(message.Content, "What are my preferences"):
			fmt.Fprint(w, `{"id":"chatcmpl-foundry-mem-3","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_load_1","type":"function","function":{"name":"load_memory","arguments":"{\"query\": \"user preferences\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
	}

	http.Error(w, "unexpected Foundry chat request", http.StatusNotFound)
}

func writeFoundryEmbeddingResponse(w http.ResponseWriter) {
	fmt.Fprintf(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":%s}],"model":"text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`, foundryVectorJSON(768))
}

func foundryVectorJSON(dimensions int) string {
	values := make([]string, dimensions)
	for i := range values {
		values[i] = "1"
	}
	return "[" + strings.Join(values, ",") + "]"
}

// TestE2EMemoryWithGoADKFoundryAgent verifies memory works end-to-end on the Go
// runtime when both the chat model and the embedding model are Azure AI Foundry
// ModelConfigs.
func TestE2EMemoryWithGoADKFoundryAgent(t *testing.T) {
	endpoint, stopServer := setupFoundryMemoryMockServer(t)
	defer stopServer()

	cli := setupK8sClient(t, false)
	chatModelCfg := setupFoundryModelConfig(t, cli, endpoint, foundryChatDeployment, "gpt-4.1-nano")
	embeddingModelCfg := setupFoundryModelConfig(t, cli, endpoint, foundryEmbeddingDeployment, "text-embedding-3-small")

	goRuntime := v1alpha2.DeclarativeRuntime_Go
	agent := setupAgentWithOptions(t, cli, chatModelCfg.Name, nil, AgentOptions{
		Name:    "memory-go-adk-foundry-test",
		Runtime: &goRuntime,
		Memory: &v1alpha2.MemorySpec{
			ModelConfig: embeddingModelCfg.Name,
		},
	})
	a2aClient := setupA2AClient(t, agent)

	var saveResult *a2atype.Task
	t.Run("save_memory", func(t *testing.T) {
		saveResult = runSyncTest(t, a2aClient,
			"Remember that I prefer dark mode and Go over Python",
			"saved your preferences to memory",
			nil,
		)
	})

	t.Run("load_memory", func(t *testing.T) {
		runSyncTest(t, a2aClient,
			"What are my preferences?",
			"dark mode",
			nil,
			saveResult.ContextID,
		)
	})
}
