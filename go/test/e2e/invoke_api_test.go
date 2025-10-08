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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

//go:embed mocks
var mocks embed.FS

// setupMockServer creates and starts a mock LLM server
func setupMockServer(t *testing.T, mockFile string) (string, func()) {
	mockllmCfg, err := mockllm.LoadConfigFromFile(mockFile, mocks)
	require.NoError(t, err)

	server := mockllm.NewServer(mockllmCfg)
	baseURL, err := server.Start()
	baseURL = buildK8sURL(baseURL)
	require.NoError(t, err)

	return baseURL, func() { server.Stop() } //nolint:errcheck
}

// setupK8sClient creates a Kubernetes client with the appropriate schemes
func setupK8sClient(t *testing.T, includeV1Alpha1 bool) client.Client {
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	scheme := k8s_runtime.NewScheme()
	err = v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	if includeV1Alpha1 {
		err = v1alpha1.AddToScheme(scheme)
		require.NoError(t, err)
	}

	cli, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	return cli
}

// setupModelConfig creates and returns a model config resource
func setupModelConfig(t *testing.T, cli client.Client, baseURL string) *v1alpha2.ModelConfig {
	modelCfg := generateModelCfg(baseURL + "/v1")
	cli.Create(t.Context(), modelCfg) //nolint:errcheck
	return modelCfg
}

// setupMCPServer creates and returns an MCP server resource
func setupMCPServer(t *testing.T, cli client.Client, mcpServer *v1alpha1.MCPServer) *v1alpha1.MCPServer {
	if mcpServer == nil {
		return nil
	}
	cli.Create(t.Context(), mcpServer) //nolint:errcheck
	return mcpServer
}

// setupAgent creates and returns an agent resource, then waits for it to be ready
func setupAgent(t *testing.T, cli client.Client, tools []*v1alpha2.Tool) *v1alpha2.Agent {
	agent := generateAgent(tools)
	cli.Create(t.Context(), agent) //nolint:errcheck

	// Wait for agent to be ready
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

	return agent
}

// setupA2AClient creates an A2A client for the test agent
func setupA2AClient(t *testing.T) *a2aclient.A2AClient {
	a2aURL := a2aUrl("kagent", "test-agent")
	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)
	return a2aClient
}

// extractTextFromArtifacts extracts all text content from task artifacts
func extractTextFromArtifacts(taskResult *protocol.Task) string {
	var text strings.Builder
	for _, artifact := range taskResult.Artifacts {
		for _, part := range artifact.Parts {
			if textPart, ok := part.(*protocol.TextPart); ok {
				text.WriteString(textPart.Text)
			}
		}
	}
	return text.String()
}

// runSyncTest runs a synchronous message test
// useArtifacts: if true, check artifacts; if false or nil, check history;
// contextID: optional context ID to maintain conversation context
func runSyncTest(t *testing.T, a2aClient *a2aclient.A2AClient, userMessage, expectedText string, useArtifacts *bool, contextID ...string) *protocol.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg := protocol.Message{
		Kind:  protocol.KindMessage,
		Role:  protocol.MessageRoleUser,
		Parts: []protocol.Part{protocol.NewTextPart(userMessage)},
	}
	
	// If contextID is provided, set it to maintain conversation context
	if len(contextID) > 0 && contextID[0] != "" {
		msg.ContextID = &contextID[0]
	}

	result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{Message: msg})
	require.NoError(t, err)

	taskResult, ok := result.Result.(*protocol.Task)
	require.True(t, ok)
	
	// Extract text based on useArtifacts flag
	if useArtifacts != nil && *useArtifacts {
		// Check artifacts (used by CrewAI flows)
		text := extractTextFromArtifacts(taskResult)
		require.Contains(t, text, expectedText)
	} else {
		// Check history (used by declarative agents) - default
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, expectedText, string(jsn))
	}
	
	return taskResult
}

// runStreamingTest runs a streaming message test
// If contextID is provided, it will be included in the message to maintain conversation context
// Checks the full JSON output to support both artifacts and history from different agent types
func runStreamingTest(t *testing.T, a2aClient *a2aclient.A2AClient, userMessage, expectedText string, contextID ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg := protocol.Message{
		Kind:  protocol.KindMessage,
		Role:  protocol.MessageRoleUser,
		Parts: []protocol.Part{protocol.NewTextPart(userMessage)},
	}
	
	// If contextID is provided, set it to maintain conversation context
	if len(contextID) > 0 && contextID[0] != "" {
		msg.ContextID = &contextID[0]
	}

	stream, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{Message: msg})
	require.NoError(t, err)

	resultList := []protocol.StreamingMessageEvent{}
	var text string
	for event := range stream {
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
	require.Contains(t, string(jsn), expectedText, string(jsn))
}

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

func generateAgent(tools []*v1alpha2.Tool) *v1alpha2.Agent {
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
				Tools:         tools,
			},
		},
	}
}

func generateMCPServer() *v1alpha1.MCPServer {
	return &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "everything-mcp-server",
			Namespace: "kagent",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Port: 3000,
				Cmd:  "npx",
				Args: []string{"-y", "@modelcontextprotocol/server-everything"},
			},
			TransportType: v1alpha1.TransportTypeStdio,
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

func TestE2EInvokeInlineAgent(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Define tools
	tools := []*v1alpha2.Tool{
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
	}

	// Setup specific resources
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupAgent(t, cli, tools)

	// Cleanup
	defer func() {
		cli.Delete(t.Context(), agent)    //nolint:errcheck
		cli.Delete(t.Context(), modelCfg) //nolint:errcheck
	}()

	// Setup A2A client
	a2aClient := setupA2AClient(t)

	// Run tests
	t.Run("sync_invocation", func(t *testing.T) {
		runSyncTest(t, a2aClient, "List all nodes in the cluster", "kagent-control-plane", nil)
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		runStreamingTest(t, a2aClient, "List all nodes in the cluster", "kagent-control-plane")
	})
}

func TestE2EInvokeExternalAgent(t *testing.T) {
	// Setup A2A client for external agent
	a2aURL := a2aUrl("kagent", "kebab-agent")
	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	// Run tests
	t.Run("sync_invocation", func(t *testing.T) {
		runSyncTest(t, a2aClient, "What can you do?", "kebab", nil)
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		runStreamingTest(t, a2aClient, "What can you do?", "kebab")
	})

	t.Run("invocation with different user", func(t *testing.T) {
		// Setup A2A client with authentication
		authClient, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithAPIKeyAuth("user@example.com", "x-user-id"))
		require.NoError(t, err)

		runSyncTest(t, authClient, "What can you do?", "kebab for user@example.com", nil)
	})
}

func TestE2EInvokeDeclarativeAgentWithMcpServerTool(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_mcp_agent.json")
	defer stopServer()

	// Setup Kubernetes client (include v1alpha1 for MCPServer)
	cli := setupK8sClient(t, true)

	// Define tools
	tools := []*v1alpha2.Tool{
		{
			Type: v1alpha2.ToolProviderType_McpServer,
			McpServer: &v1alpha2.McpServerTool{
				TypedLocalReference: v1alpha2.TypedLocalReference{
					ApiGroup: "kagent.dev",
					Kind:     "MCPServer",
					Name:     "everything-mcp-server",
				},
				ToolNames: []string{"add"},
			},
		},
	}

	// Setup specific resources
	modelCfg := setupModelConfig(t, cli, baseURL)
	mcpServer := setupMCPServer(t, cli, generateMCPServer())
	agent := setupAgent(t, cli, tools)

	// Cleanup
	defer func() {
		cli.Delete(t.Context(), agent)     //nolint:errcheck
		cli.Delete(t.Context(), mcpServer) //nolint:errcheck
		cli.Delete(t.Context(), modelCfg)  //nolint:errcheck
	}()

	// Setup A2A client
	a2aClient := setupA2AClient(t)

	// Run tests
	t.Run("sync_invocation", func(t *testing.T) {
		runSyncTest(t, a2aClient, "add 3 and 5", "8", nil)
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		runStreamingTest(t, a2aClient, "add 3 and 5", "8")
	})
}

// This function generates a CrewAI agent that uses a mock LLM server
// Assumes that the image is built and pushed to registry, the agent can be found in python/samples/crewai/poem_flow
func generateCrewAIAgent(baseURL string) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "poem-flow-test",
			Namespace: "kagent",
		},
		Spec: v1alpha2.AgentSpec{
			Description: "A flow that uses a crew to generate a poem.",
			Type:        v1alpha2.AgentType_BYO,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image: "localhost:5001/poem-flow:latest",
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
					Env: []corev1.EnvVar{
						{
							Name: "OPENAI_API_KEY",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "kagent-openai",
									},
									Key: "OPENAI_API_KEY",
								},
							},
						},
						// Inject the mock server's URL, CrewAI uses this environment variable
						{
							Name: "OPENAI_API_BASE",
							Value: baseURL + "/v1",
						},
					},
				},
				},
			},
		},
	}
}

func TestE2EInvokeCrewAIAgent(t *testing.T) {
	mockllmCfg, err := mockllm.LoadConfigFromFile("mocks/invoke_crewai_agent.json", mocks)
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
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	cli, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	// Clean up any leftover agent from a previous failed run
	_ = cli.Delete(t.Context(), &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "poem-flow-test", Namespace: "kagent"}})

	// Generate the CrewAI agent and inject the mock server's URL
	agent := generateCrewAIAgent(baseURL)

	// Create the agent on the cluster
	err = cli.Create(t.Context(), agent)
	require.NoError(t, err)

	defer func() {
		cli.Delete(t.Context(), agent) //nolint:errcheck
	}()

	// Wait for the agent to become Ready
	args := []string{
		"wait",
		"--for",
		"condition=Ready",
		"--timeout=1m",
		"agents.kagent.dev",
		agent.Name,
		"-n",
		agent.Namespace,
	}

	cmd := exec.CommandContext(t.Context(), "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// Give the agent pod extra time to fully initialize its A2A endpoint
	time.Sleep(5 * time.Second)

	// Setup A2A client
	a2aURL := a2aUrl(agent.Namespace, agent.Name)
	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("two_turn_conversation", func(t *testing.T) {
		// First turn: Generate initial poem
		// Use artifacts only (true) for CrewAI flows
		useArtifacts := true
		taskResult1 := runSyncTest(t, a2aClient, "Generate a poem about CrewAI", "CrewAI is awesome, it makes coding fun.", &useArtifacts)

		// Second turn: Continue poem (tests persistence)
		// Use the same ContextID to maintain conversation context
		runSyncTest(t, a2aClient, "Continue the poem", "In harmony with the code, it flows so smooth.", &useArtifacts, taskResult1.ContextID)
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		runStreamingTest(t, a2aClient, "Generate a poem about CrewAI", "CrewAI is awesome, it makes coding fun.")
	})
}
