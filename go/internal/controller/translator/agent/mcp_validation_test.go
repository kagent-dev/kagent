package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

// TestMCPServerValidation_InvalidPort tests that TranslateAgent fails when an Agent
// references an MCPServer with an invalid (zero) port configuration.
func TestMCPServerValidation_InvalidPort(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create an MCPServer with invalid port (0)
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  0, // Invalid port
			},
			TransportType: "stdio",
		},
	}

	// Create an Agent that references the MCPServer
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{},
						},
					},
				},
			},
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, mcpServer, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should fail with error about invalid port
	_, err = translator.TranslateAgent(ctx, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine port")
	assert.Contains(t, err.Error(), "test-mcp-server")
}

// TestMCPServerValidation_ValidPort tests that TranslateAgent succeeds when an Agent
// references an MCPServer with a valid port configuration.
func TestMCPServerValidation_ValidPort(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create an MCPServer with valid port
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  8080, // Valid port
			},
			TransportType: "stdio",
		},
	}

	// Create an Agent that references the MCPServer
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{},
						},
					},
				},
			},
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, mcpServer, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should succeed
	outputs, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	assert.NotNil(t, outputs)
	assert.NotNil(t, outputs.Config)
}

// TestMCPServerValidation_NotFound tests that TranslateAgent fails when an Agent
// references an MCPServer that does not exist.
func TestMCPServerValidation_NotFound(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create an Agent that references a non-existent MCPServer
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "non-existent-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{},
						},
					},
				},
			},
		},
	}

	// Create fake client with test objects (no MCPServer)
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should fail with not found error
	_, err = translator.TranslateAgent(ctx, agent)
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err))
}

// TestMCPServerValidation_NoMCPServerReference tests that TranslateAgent fails
// when a tool of type McpServer is missing the mcpServer reference.
func TestMCPServerValidation_NoMCPServerReference(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create an Agent with a tool that has type McpServer but no mcpServer reference
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type:      v1alpha2.ToolProviderType_McpServer,
						McpServer: nil, // Missing reference
					},
				},
			},
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should fail with provider or tool server error
	_, err = translator.TranslateAgent(ctx, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool must have a provider or tool server")
}

// TestMCPServerValidation_RemoteMCPServer tests that validation also works for RemoteMCPServer.
func TestMCPServerValidation_RemoteMCPServer(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create a RemoteMCPServer
	remoteMcpServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-remote-mcp",
			Namespace: "test",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://external-mcp-server:8080/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}

	// Create an Agent that references the RemoteMCPServer
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "test-remote-mcp",
								Kind: "RemoteMCPServer",
							},
							ToolNames: []string{},
						},
					},
				},
			},
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, remoteMcpServer, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should succeed - RemoteMCPServer doesn't have port validation
	outputs, err := translator.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	assert.NotNil(t, outputs)
	assert.NotNil(t, outputs.Config)
}

// TestConvertMCPServerToRemoteMCPServer_InvalidPort tests the conversion function directly.
func TestConvertMCPServerToRemoteMCPServer_InvalidPort(t *testing.T) {
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  0, // Invalid port
			},
			TransportType: "stdio",
		},
	}

	_, err := agenttranslator.ConvertMCPServerToRemoteMCPServer(mcpServer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine port")
	assert.Contains(t, err.Error(), "test-mcp-server")

	// Verify the error is a ValidationError
	var validationErr *agenttranslator.ValidationError
	assert.True(t, errors.As(err, &validationErr), "Error should be a ValidationError")
}

// TestConvertMCPServerToRemoteMCPServer_ValidPort tests the conversion function with valid port.
func TestConvertMCPServerToRemoteMCPServer_ValidPort(t *testing.T) {
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  8080, // Valid port
			},
			TransportType: "stdio",
		},
	}

	remoteMCP, err := agenttranslator.ConvertMCPServerToRemoteMCPServer(mcpServer)
	require.NoError(t, err)
	assert.NotNil(t, remoteMCP)
	assert.Equal(t, "http://test-mcp-server.test:8080/mcp", remoteMCP.Spec.URL)
	assert.Equal(t, v1alpha2.RemoteMCPServerProtocolStreamableHttp, remoteMCP.Spec.Protocol)
}

// TestMCPServerValidation_MultipleTools tests validation with multiple tools including valid and invalid MCPServers.
func TestMCPServerValidation_MultipleTools(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create a ModelConfig for the agent
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

	// Create a valid MCPServer
	validMcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  8080, // Valid port
			},
			TransportType: "stdio",
		},
	}

	// Create an invalid MCPServer
	invalidMcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  0, // Invalid port
			},
			TransportType: "stdio",
		},
	}

	// Create an Agent that references both MCPServers
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "valid-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{},
						},
					},
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								Name: "invalid-mcp-server",
								Kind: "MCPServer",
							},
							ToolNames: []string{},
						},
					},
				},
			},
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, validMcpServer, invalidMcpServer, agent).
		Build()

	// Create translator
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)

	// TranslateAgent should fail because one of the MCPServers is invalid
	_, err = translator.TranslateAgent(ctx, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine port")
	assert.Contains(t, err.Error(), "invalid-mcp-server")
}
