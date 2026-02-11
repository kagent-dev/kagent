package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/pkg/adk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Test_AdkApiTranslator_CrossNamespaceAgentTool tests that the translator can
// handle cross-namespace agent references. Note that cross-namespace validation
// (e.g. AllowedNamespaces checks) is a concern of the reconciler, not the
// translator; the translator just performs the translation.
func Test_AdkApiTranslator_CrossNamespaceAgentTool(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	// Create test namespaces
	sourceNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source-ns",
			Labels: map[string]string{
				"shared-agent-access": "true",
			},
		},
	}
	targetNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "target-ns",
		},
	}

	// Create model config in source namespace
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "source-ns",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	tests := []struct {
		name        string
		toolAgent   *v1alpha2.Agent
		sourceAgent *v1alpha2.Agent
		wantErr     bool
		errContains string
	}{
		{
			name: "Same namespace reference - works",
			toolAgent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tool-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Tool agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are a tool agent",
						ModelConfig:   "test-model",
					},
				},
			},
			sourceAgent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-agent",
					Namespace: "source-ns", // Same namespace as tool agent
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Source agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are a source agent",
						ModelConfig:   "test-model",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedLocalReference{
									Name:      "tool-agent",
									Namespace: "source-ns",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Cross-namespace reference - translates successfully (validation is in reconciler)",
			toolAgent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tool-agent",
					Namespace: "target-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Tool agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are a tool agent",
						ModelConfig:   "test-model",
					},
				},
			},
			sourceAgent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-agent",
					Namespace: "source-ns", // Different namespace from tool agent
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Source agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are a source agent",
						ModelConfig:   "test-model",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedLocalReference{
									Name:      "tool-agent",
									Namespace: "target-ns",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(
					sourceNs,
					targetNs,
					tt.toolAgent,
					tt.sourceAgent,
				)

			// Create model config in source agent namespace
			modelConfigSource := modelConfig.DeepCopy()
			modelConfigSource.Namespace = tt.sourceAgent.Namespace
			clientBuilder = clientBuilder.WithObjects(modelConfigSource)

			// Also need model config in tool agent namespace for the tool agent to be valid (if different)
			if tt.toolAgent.Namespace != tt.sourceAgent.Namespace {
				toolModelConfig := modelConfig.DeepCopy()
				toolModelConfig.Namespace = tt.toolAgent.Namespace
				clientBuilder = clientBuilder.WithObjects(toolModelConfig)
			}

			kubeClient := clientBuilder.Build()

			defaultModel := types.NamespacedName{
				Namespace: tt.sourceAgent.Namespace,
				Name:      "test-model",
			}

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

			_, err := trans.TranslateAgent(context.Background(), tt.sourceAgent)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

// Test_AdkApiTranslator_CrossNamespaceRemoteMCPServer tests that the translator
// can handle cross-namespace RemoteMCPServer references. Note that cross-namespace
// validation (AllowedNamespaces checks) is now done in the reconciler,
// not the translator. The translator just performs the translation.
func Test_AdkApiTranslator_CrossNamespaceRemoteMCPServer(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	// Create test namespaces
	sourceNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source-ns",
			Labels: map[string]string{
				"shared-tools-access": "true",
			},
		},
	}
	targetNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "target-ns",
		},
	}

	// Create model config
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "source-ns",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	tests := []struct {
		name            string
		remoteMCPServer *v1alpha2.RemoteMCPServer
		agent           *v1alpha2.Agent
		wantErr         bool
		errContains     string
	}{
		{
			name: "Same namespace reference - works",
			remoteMCPServer: &v1alpha2.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tools-server",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.RemoteMCPServerSpec{
					Description: "Tools server",
					URL:         "http://tools.example.com/mcp",
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are an agent",
						ModelConfig:   "test-model",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedLocalReference: v1alpha2.TypedLocalReference{
										Kind:      "RemoteMCPServer",
										ApiGroup:  "kagent.dev",
										Name:      "tools-server",
										Namespace: "source-ns",
									},
									ToolNames: []string{"tool1"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Cross-namespace reference - translates successfully (validation is in reconciler)",
			remoteMCPServer: &v1alpha2.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tools-server",
					Namespace: "target-ns",
				},
				Spec: v1alpha2.RemoteMCPServerSpec{
					Description: "Tools server",
					URL:         "http://tools.example.com/mcp",
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "You are an agent",
						ModelConfig:   "test-model",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedLocalReference: v1alpha2.TypedLocalReference{
										Kind:      "RemoteMCPServer",
										ApiGroup:  "kagent.dev",
										Name:      "tools-server",
										Namespace: "target-ns",
									},
									ToolNames: []string{"tool1"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(
					sourceNs,
					targetNs,
					modelConfig,
					tt.remoteMCPServer,
					tt.agent,
				).
				Build()

			defaultModel := types.NamespacedName{
				Namespace: tt.agent.Namespace,
				Name:      "test-model",
			}

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

			_, err := trans.TranslateAgent(context.Background(), tt.agent)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func Test_AdkApiTranslator_OllamaOptions(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := "test-ns"
	modelName := "ollama-model"
	agentName := "test-agent"

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "llama2",
			Provider: v1alpha2.ModelProviderOllama,
			Ollama: &v1alpha2.OllamaConfig{
				Host: "http://ollama:11434",
				Options: map[string]string{
					"num_ctx":     "4096",
					"temperature": "0.7",
				},
			},
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Test Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "System message",
				ModelConfig:   modelName,
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, modelConfig, agent).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: namespace,
		Name:      modelName,
	}

	trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

	outputs, err := trans.TranslateAgent(context.Background(), agent)
	require.NoError(t, err)
	require.NotNil(t, outputs)
	require.NotNil(t, outputs.Config)

	ollamaModel, ok := outputs.Config.Model.(*adk.Ollama)
	require.True(t, ok, "Expected model to be of type Ollama")

	assert.Equal(t, "4096", ollamaModel.Options["num_ctx"])
	assert.Equal(t, "0.7", ollamaModel.Options["temperature"])
}

func Test_AdkApiTranslator_ServiceAccountNameOverride(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	tests := []struct {
		name                   string
		agent                  *v1alpha2.Agent
		expectedServiceAccount string
	}{
		{
			name: "Default Service Account (Agent Name)",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Default Agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "System message",
						ModelConfig:   "test-model",
					},
				},
			},
			expectedServiceAccount: "default-agent",
		},
		{
			name: "Custom Service Account in Declarative Agent",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-sa-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Custom SA Agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "System message",
						ModelConfig:   "test-model",
						Deployment: &v1alpha2.DeclarativeDeploymentSpec{
							SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
								ServiceAccountName: ptr.To("custom-sa"),
							},
						},
					},
				},
			},
			expectedServiceAccount: "custom-sa",
		},
		{
			name: "Default Service Account with Labels and Annotations",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "configured-sa-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Agent with configured SA",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "System message",
						ModelConfig:   "test-model",
						Deployment: &v1alpha2.DeclarativeDeploymentSpec{
							SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
								ServiceAccountConfig: &v1alpha2.ServiceAccountConfig{
									Labels: map[string]string{
										"custom-label": "value",
									},
									Annotations: map[string]string{
										"custom-annotation": "value",
									},
								},
							},
						},
					},
				},
			},
			expectedServiceAccount: "configured-sa-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelConfig := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-model",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
				},
			}

			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, tt.agent).
				Build()

			defaultModel := types.NamespacedName{
				Namespace: "default",
				Name:      "test-model",
			}

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

			outputs, err := trans.TranslateAgent(context.Background(), tt.agent)
			require.NoError(t, err)
			require.NotNil(t, outputs)

			// Find Deployment and ServiceAccount in Manifest
			var deployment *appsv1.Deployment
			var serviceAccount *corev1.ServiceAccount
			for _, obj := range outputs.Manifest {
				if d, ok := obj.(*appsv1.Deployment); ok {
					deployment = d
				}
				if sa, ok := obj.(*corev1.ServiceAccount); ok {
					serviceAccount = sa
				}
			}

			require.NotNil(t, deployment, "Deployment should be created")
			assert.Equal(t, tt.expectedServiceAccount, deployment.Spec.Template.Spec.ServiceAccountName)

			// If the custom SA name matches agent name, it should be created. Otherwise, it should be skipped.
			if tt.expectedServiceAccount == tt.agent.Name {
				assert.NotNil(t, serviceAccount, "ServiceAccount should be created when using default name")

				// Verify Config if present
				var saConfig *v1alpha2.ServiceAccountConfig
				switch tt.agent.Spec.Type {
				case v1alpha2.AgentType_Declarative:
					if tt.agent.Spec.Declarative.Deployment != nil {
						saConfig = tt.agent.Spec.Declarative.Deployment.ServiceAccountConfig
					}
				case v1alpha2.AgentType_BYO:
					if tt.agent.Spec.BYO.Deployment != nil {
						saConfig = tt.agent.Spec.BYO.Deployment.ServiceAccountConfig
					}
				}

				if saConfig != nil && serviceAccount != nil {
					for k, v := range saConfig.Labels {
						assert.Equal(t, v, serviceAccount.Labels[k], "Label %s mismatch", k)
					}
					for k, v := range saConfig.Annotations {
						assert.Equal(t, v, serviceAccount.Annotations[k], "Annotation %s mismatch", k)
					}
				}
			} else {
				assert.Nil(t, serviceAccount, "ServiceAccount should NOT be created when using custom override")
			}

			// Verify KAGENT_NAME env var
			var kagentNameEnv string
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "KAGENT_NAME" {
					if env.Value != "" {
						kagentNameEnv = env.Value
					} else if env.ValueFrom != nil && env.ValueFrom.FieldRef != nil {
						kagentNameEnv = "Ref:" + env.ValueFrom.FieldRef.FieldPath
					}
				}
			}
			assert.Equal(t, tt.agent.Name, kagentNameEnv, "KAGENT_NAME env var should be the agent name")
		})
	}
}
