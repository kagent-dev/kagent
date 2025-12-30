package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
)

// TestProxyConfiguration_ThroughTranslateAgent tests proxy URL rewriting through the public API
func TestProxyConfiguration_ThroughTranslateAgent(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	// Create test objects
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-model",
			Namespace: "test",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: "OpenAI",
			Model:    "gpt-4o",
		},
	}

	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://test-mcp-server.kagent:8084/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}

	nestedAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nested-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test",
				ModelConfig:   "default-model",
			},
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_Agent,
						Agent: &v1alpha2.TypedLocalReference{
							Name: "nested-agent",
						},
					},
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-mcp",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, nestedAgent, remoteMcpServer, modelConfig).
		Build()

	t.Run("with proxy URLs", func(t *testing.T) {
		translator := translator.NewAdkApiTranslator(
			kubeClient,
			types.NamespacedName{Name: "default-model", Namespace: "test"},
			nil,
			"http://agent-a2a-proxy:8081",
			"http://agent-egress-proxy:8082",
		)

		result, err := translator.TranslateAgent(ctx, agent)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Config)

		// Verify A2A proxy configuration
		require.Len(t, result.Config.RemoteAgents, 1)
		remoteAgent := result.Config.RemoteAgents[0]
		assert.Equal(t, "http://agent-a2a-proxy:8081", remoteAgent.Url)
		assert.NotNil(t, remoteAgent.Headers)
		assert.Equal(t, "nested-agent.test", remoteAgent.Headers["Host"])

		// Verify egress proxy configuration
		require.Len(t, result.Config.HttpTools, 1)
		httpTool := result.Config.HttpTools[0]
		assert.Equal(t, "http://agent-egress-proxy:8082/mcp", httpTool.Params.Url)
		assert.NotNil(t, httpTool.Params.Headers)
		assert.Equal(t, "test-mcp-server.kagent", httpTool.Params.Headers["Host"])
	})

	t.Run("without proxy URLs", func(t *testing.T) {
		translator := translator.NewAdkApiTranslator(
			kubeClient,
			types.NamespacedName{Name: "default-model", Namespace: "test"},
			nil,
			"", // No A2A proxy
			"", // No egress proxy
		)

		result, err := translator.TranslateAgent(ctx, agent)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Config)

		// Verify A2A direct URL (no proxy)
		require.Len(t, result.Config.RemoteAgents, 1)
		remoteAgent := result.Config.RemoteAgents[0]
		assert.Equal(t, "http://nested-agent.test:8080", remoteAgent.Url)
		// Host header should not be set when no proxy
		if remoteAgent.Headers != nil {
			_, hasHost := remoteAgent.Headers["Host"]
			assert.False(t, hasHost, "Host header should not be set when no proxy")
		}

		// Verify egress direct URL (no proxy)
		require.Len(t, result.Config.HttpTools, 1)
		httpTool := result.Config.HttpTools[0]
		assert.Equal(t, "http://test-mcp-server.kagent:8084/mcp", httpTool.Params.Url)
		// Host header should not be set when no proxy
		if httpTool.Params.Headers != nil {
			_, hasHost := httpTool.Params.Headers["Host"]
			assert.False(t, hasHost, "Host header should not be set when no proxy")
		}
	})
}
