package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
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

	// Add namespaces to fake client so namespace existence checks work
	kagentNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kagent",
		},
	}
	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, nestedAgent, remoteMcpServer, modelConfig, kagentNamespace, testNamespace).
		Build()

	t.Run("with proxy URL - RemoteMCPServer with internal k8s URL uses proxy", func(t *testing.T) {
		translator := agenttranslator.NewAdkApiTranslator(
			kubeClient,
			types.NamespacedName{Name: "default-model", Namespace: "test"},
			nil,
			"http://proxy.kagent.svc.cluster.local:8080",
		)

		result, err := translator.TranslateAgent(ctx, agent)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Config)

		// Verify agent tool proxy configuration
		require.Len(t, result.Config.RemoteAgents, 1)
		remoteAgent := result.Config.RemoteAgents[0]
		assert.Equal(t, "http://proxy.kagent.svc.cluster.local:8080", remoteAgent.Url)
		assert.NotNil(t, remoteAgent.Headers)
		assert.Equal(t, "nested-agent.test", remoteAgent.Headers[agenttranslator.ProxyHostHeader])

		// Verify RemoteMCPServer with internal k8s URL DOES use proxy
		require.Len(t, result.Config.HttpTools, 1)
		httpTool := result.Config.HttpTools[0]
		assert.Equal(t, "http://proxy.kagent.svc.cluster.local:8080/mcp", httpTool.Params.Url)
		// Proxy header should be set for RemoteMCPServer with internal k8s URL (uses proxy)
		require.NotNil(t, httpTool.Params.Headers)
		assert.Equal(t, "test-mcp-server.kagent", httpTool.Params.Headers[agenttranslator.ProxyHostHeader])
	})

	t.Run("without proxy URL", func(t *testing.T) {
		translator := agenttranslator.NewAdkApiTranslator(
			kubeClient,
			types.NamespacedName{Name: "default-model", Namespace: "test"},
			nil,
			"", // No proxy
		)

		result, err := translator.TranslateAgent(ctx, agent)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Config)

		// Verify agent tool direct URL (no proxy)
		require.Len(t, result.Config.RemoteAgents, 1)
		remoteAgent := result.Config.RemoteAgents[0]
		assert.Equal(t, "http://nested-agent.test:8080", remoteAgent.Url)
		// Proxy header should not be set when no proxy
		if remoteAgent.Headers != nil {
			_, hasHost := remoteAgent.Headers[agenttranslator.ProxyHostHeader]
			assert.False(t, hasHost, "Proxy header should not be set when no proxy")
		}

		// Verify RemoteMCPServer direct URL (no proxy)
		require.Len(t, result.Config.HttpTools, 1)
		httpTool := result.Config.HttpTools[0]
		assert.Equal(t, "http://test-mcp-server.kagent:8084/mcp", httpTool.Params.Url)
		// Proxy header should not be set when no proxy
		if httpTool.Params.Headers != nil {
			_, hasHost := httpTool.Params.Headers[agenttranslator.ProxyHostHeader]
			assert.False(t, hasHost, "Proxy header should not be set when no proxy")
		}
	})
}

// TestProxyConfiguration_RemoteMCPServer_ExternalURL tests that RemoteMCPServer with external URLs does NOT use proxy
func TestProxyConfiguration_RemoteMCPServer_ExternalURL(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	// RemoteMCPServer with external URL (not internal k8s)
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-mcp",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "https://external-mcp.example.com/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "external-mcp",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, remoteMcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"http://proxy.kagent.svc.cluster.local:8080",
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify RemoteMCPServer with external URL does NOT use proxy
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "https://external-mcp.example.com/mcp", httpTool.Params.Url)
	// Proxy header should not be set for external URLs (no proxy)
	if httpTool.Params.Headers != nil {
		_, hasHost := httpTool.Params.Headers[agenttranslator.ProxyHostHeader]
		assert.False(t, hasHost, "Proxy header should not be set for RemoteMCPServer with external URL (no proxy)")
	}
}

// TestProxyConfiguration_MCPServer tests that MCPServer resources use proxy
func TestProxyConfiguration_MCPServer(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

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

	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Port: 8084,
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, mcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"http://proxy.kagent.svc.cluster.local:8080",
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify MCPServer uses proxy
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "http://proxy.kagent.svc.cluster.local:8080/mcp", httpTool.Params.Url)
	// Proxy header should be set for MCPServer (uses proxy)
	require.NotNil(t, httpTool.Params.Headers)
	assert.Equal(t, "test-mcp-server.test", httpTool.Params.Headers[agenttranslator.ProxyHostHeader])
}

// TestProxyConfiguration_Service tests that Services as MCP Tools use proxy
func TestProxyConfiguration_Service(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test",
			Annotations: map[string]string{
				"kagent.dev/mcp-service-port":     "8084",
				"kagent.dev/mcp-service-path":     "/mcp",
				"kagent.dev/mcp-service-protocol": "streamable-http",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "mcp",
					Port:     8084,
					Protocol: corev1.ProtocolTCP,
				},
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-service",
								Kind: "Service",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, service, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"http://proxy.kagent.svc.cluster.local:8080",
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify Service uses proxy
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "http://proxy.kagent.svc.cluster.local:8080/mcp", httpTool.Params.Url)
	// Proxy header should be set for Service (uses proxy)
	require.NotNil(t, httpTool.Params.Headers)
	assert.Equal(t, "test-service.test", httpTool.Params.Headers[agenttranslator.ProxyHostHeader])
}

// TestTLSConfiguration_URLConversion tests that URLs are converted from http to https when TLS is configured
func TestTLSConfiguration_URLConversion(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	// RemoteMCPServer with TLS configuration and http:// URL
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-mcp",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://kyverno-mcp.default:8000/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			TLS: &v1alpha2.TLSConfig{
				ClientSecretRef: "client-cert-secret",
				DisableVerify:  false,
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "tls-mcp",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, remoteMcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"", // No proxy
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify URL is converted from http to https when TLS is configured
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "https://kyverno-mcp.default:8000/mcp", httpTool.Params.Url, "URL should be converted from http to https when TLS is configured")
	
	// Verify TLS configuration is populated
	assert.NotNil(t, httpTool.Params.TLSClientCertPath, "TLS client cert path should be set")
}

// TestTLSConfiguration_URLConversion_AlreadyHTTPS tests that https:// URLs are not changed
func TestTLSConfiguration_URLConversion_AlreadyHTTPS(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	// RemoteMCPServer with TLS configuration and already https:// URL
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-mcp-https",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "https://external-mcp.example.com:8443/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			TLS: &v1alpha2.TLSConfig{
				ClientSecretRef: "client-cert-secret",
				DisableVerify:  false,
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "tls-mcp-https",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, remoteMcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"", // No proxy
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify https:// URL is not changed
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "https://external-mcp.example.com:8443/mcp", httpTool.Params.Url, "https:// URL should remain unchanged")
}

// TestTLSConfiguration_URLConversion_NoTLS tests that URLs remain http:// when no TLS is configured
func TestTLSConfiguration_URLConversion_NoTLS(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	// RemoteMCPServer without TLS configuration
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-tls-mcp",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://kyverno-mcp.default:8000/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			TLS:      nil, // No TLS
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "no-tls-mcp",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, remoteMcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"", // No proxy
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify http:// URL remains unchanged when no TLS is configured
	require.Len(t, result.Config.HttpTools, 1)
	httpTool := result.Config.HttpTools[0]
	assert.Equal(t, "http://kyverno-mcp.default:8000/mcp", httpTool.Params.Url, "http:// URL should remain unchanged when no TLS is configured")
}

// TestTLSConfiguration_URLConversion_SSE tests that SSE protocol also converts http to https
func TestTLSConfiguration_URLConversion_SSE(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

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

	// RemoteMCPServer with TLS configuration and SSE protocol
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-mcp-sse",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://kyverno-mcp.default:8000/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolSse,
			TLS: &v1alpha2.TLSConfig{
				ClientSecretRef: "client-cert-secret",
				DisableVerify:  false,
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
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "tls-mcp-sse",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{"test-tool"},
						},
					},
				},
			},
		},
	}

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, remoteMcpServer, modelConfig, testNamespace).
		Build()

	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		"", // No proxy
	)

	result, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Config)

	// Verify URL is converted from http to https for SSE protocol when TLS is configured
	require.Len(t, result.Config.SseTools, 1)
	sseTool := result.Config.SseTools[0]
	assert.Equal(t, "https://kyverno-mcp.default:8000/mcp", sseTool.Params.Url, "URL should be converted from http to https for SSE protocol when TLS is configured")
	
	// Verify TLS configuration is populated
	assert.NotNil(t, sseTool.Params.TLSClientCertPath, "TLS client cert path should be set")
}
