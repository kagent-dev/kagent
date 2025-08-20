package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	adk "github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	databasefake "github.com/kagent-dev/kagent/go/internal/database/fake"
)

// MockA2AReconciler is a mock implementation of the A2A reconciler interface
type MockA2AReconciler struct {
	mock.Mock
}

func (m *MockA2AReconciler) ReconcileAgentDeletion(agentName string) {
	m.Called(agentName)
}

func (m *MockA2AReconciler) ReconcileAgent(ctx context.Context, agent *v1alpha2.Agent, adkConfig *adk.AgentConfig) error {
	args := m.Called(ctx, agent, adkConfig)
	return args.Error(0)
}

// setupTestReconciler creates a test reconciler with mocked dependencies
func setupTestReconciler(t *testing.T, objects ...client.Object) (*kagentReconciler, client.Client, *databasefake.InMemmoryFakeClient, *MockA2AReconciler) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	dbClient := databasefake.NewClient()
	mockA2A := &MockA2AReconciler{}

	reconciler := &kagentReconciler{
		kube:          fakeClient,
		dbClient:      dbClient,
		a2aReconciler: mockA2A,
	}

	return reconciler, fakeClient, dbClient.(*databasefake.InMemmoryFakeClient), mockA2A
}

// createTestAgent creates a test Agent resource
func createTestAgent(name, namespace, modelConfigName string) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.AgentSpec{
			ModelConfig: modelConfigName,
		},
	}
}

// createTestModelConfig creates a test ModelConfig resource
func createTestModelConfig(name, namespace string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: "OpenAI",
		},
	}
}

// createTestRemoteMCPServer creates a test RemoteMCPServer resource
func createTestRemoteMCPServer(name, namespace string) *v1alpha2.RemoteMCPServer {
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Test MCP Server",
		},
	}
}

func TestEnsureAgentFinalizer(t *testing.T) {
	tests := []struct {
		name           string
		agent          *v1alpha2.Agent
		expectedResult bool
		expectError    bool
	}{
		{
			name:           "adds finalizer to agent without finalizers",
			agent:          createTestAgent("test-agent", "default", "test-model"),
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "skips agent that already has finalizer",
			agent: func() *v1alpha2.Agent {
				agent := createTestAgent("test-agent", "default", "test-model")
				agent.Finalizers = []string{AgentFinalizer}
				return agent
			}(),
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "adds finalizer to agent with other finalizers",
			agent: func() *v1alpha2.Agent {
				agent := createTestAgent("test-agent", "default", "test-model")
				agent.Finalizers = []string{"other-finalizer"}
				return agent
			}(),
			expectedResult: true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler, _, _, _ := setupTestReconciler(t, tt.agent)
			ctx := context.Background()

			err := reconciler.ensureAgentFinalizer(ctx, tt.agent)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tt.expectedResult {
				assert.Contains(t, tt.agent.Finalizers, AgentFinalizer)
			}
		})
	}
}

func TestHandleAgentDeletionWithFinalizer(t *testing.T) {
	tests := []struct {
		name        string
		agent       *v1alpha2.Agent
		setupMocks  func(*MockA2AReconciler, *databasefake.InMemmoryFakeClient)
		expectError bool
	}{
		{
			name: "successfully handles agent deletion with finalizer",
			agent: func() *v1alpha2.Agent {
				agent := createTestAgent("test-agent", "default", "test-model")
				agent.Finalizers = []string{AgentFinalizer}
				now := metav1.Now()
				agent.DeletionTimestamp = &now
				return agent
			}(),
			setupMocks: func(mockA2A *MockA2AReconciler, dbClient *databasefake.InMemmoryFakeClient) {
				mockA2A.On("ReconcileAgentDeletion", "default/test-agent").Return()
				// Pre-populate database with agent to delete
				dbClient.UpsertAgent(&database.Agent{
					ID: "default/test-agent",
				})
			},
			expectError: false,
		},
		{
			name: "skips agent without finalizer",
			agent: func() *v1alpha2.Agent {
				agent := createTestAgent("test-agent", "default", "test-model")
				now := metav1.Now()
				agent.DeletionTimestamp = &now
				agent.Finalizers = []string{"other-test-finalizer"}
				return agent
			}(),
			setupMocks: func(mockA2A *MockA2AReconciler, dbClient *databasefake.InMemmoryFakeClient) {
				// No mocks should be called
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler, _, dbClient, mockA2A := setupTestReconciler(t, tt.agent)
			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.agent.Name,
					Namespace: tt.agent.Namespace,
				},
			}

			tt.setupMocks(mockA2A, dbClient)

			err := reconciler.handleAgentDeletionWithFinalizer(ctx, tt.agent, req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockA2A.AssertExpectations(t)
		})
	}
}

func TestEnsureModelConfigFinalizer(t *testing.T) {
	tests := []struct {
		name           string
		modelConfig    *v1alpha2.ModelConfig
		expectedResult bool
		expectError    bool
	}{
		{
			name:           "adds finalizer to modelconfig without finalizers",
			modelConfig:    createTestModelConfig("test-model", "default"),
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "skips modelconfig that already has finalizer",
			modelConfig: func() *v1alpha2.ModelConfig {
				mc := createTestModelConfig("test-model", "default")
				mc.Finalizers = []string{ModelConfigFinalizer}
				return mc
			}(),
			expectedResult: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler, _, _, _ := setupTestReconciler(t, tt.modelConfig)
			ctx := context.Background()

			err := reconciler.ensureModelConfigFinalizer(ctx, tt.modelConfig)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tt.expectedResult {
				assert.Contains(t, tt.modelConfig.Finalizers, ModelConfigFinalizer)
			}
		})
	}
}

func TestHandleModelConfigDeletionWithFinalizer(t *testing.T) {
	tests := []struct {
		name         string
		modelConfig  *v1alpha2.ModelConfig
		agents       []*v1alpha2.Agent
		expectError  bool
		errorMessage string
	}{
		{
			name: "successfully deletes modelconfig with no dependent agents",
			modelConfig: func() *v1alpha2.ModelConfig {
				mc := createTestModelConfig("test-model", "default")
				mc.Finalizers = []string{ModelConfigFinalizer}
				now := metav1.Now()
				mc.DeletionTimestamp = &now
				return mc
			}(),
			agents:      []*v1alpha2.Agent{},
			expectError: false,
		},
		{
			name: "prevents deletion when agents still reference modelconfig",
			modelConfig: func() *v1alpha2.ModelConfig {
				mc := createTestModelConfig("test-model", "default")
				mc.Finalizers = []string{ModelConfigFinalizer}
				now := metav1.Now()
				mc.DeletionTimestamp = &now
				return mc
			}(),
			agents: []*v1alpha2.Agent{
				createTestAgent("agent1", "default", "test-model"),
			},
			expectError:  true,
			errorMessage: "still referenced by Agents",
		},
		{
			name: "skips modelconfig without finalizer",
			modelConfig: func() *v1alpha2.ModelConfig {
				mc := createTestModelConfig("test-model", "default")
				now := metav1.Now()
				mc.DeletionTimestamp = &now
				mc.Finalizers = []string{"other-test-finalizer"}
				return mc
			}(),
			agents:      []*v1alpha2.Agent{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{tt.modelConfig}
			for _, agent := range tt.agents {
				objects = append(objects, agent)
			}

			reconciler, _, _, _ := setupTestReconciler(t, objects...)
			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.modelConfig.Name,
					Namespace: tt.modelConfig.Namespace,
				},
			}

			err := reconciler.handleModelConfigDeletionWithFinalizer(ctx, tt.modelConfig, req)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureRemoteMCPServerFinalizer(t *testing.T) {
	tests := []struct {
		name           string
		server         *v1alpha2.RemoteMCPServer
		expectedResult bool
		expectError    bool
	}{
		{
			name:           "adds finalizer to server without finalizers",
			server:         createTestRemoteMCPServer("test-server", "default"),
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "skips server that already has finalizer",
			server: func() *v1alpha2.RemoteMCPServer {
				server := createTestRemoteMCPServer("test-server", "default")
				server.Finalizers = []string{RemoteMCPServerFinalizer}
				return server
			}(),
			expectedResult: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler, _, _, _ := setupTestReconciler(t, tt.server)
			ctx := context.Background()

			err := reconciler.ensureRemoteMCPServerFinalizer(ctx, tt.server)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tt.expectedResult {
				assert.Contains(t, tt.server.Finalizers, RemoteMCPServerFinalizer)
			}
		})
	}
}

func TestHandleRemoteMCPServerDeletionWithFinalizer(t *testing.T) {
	tests := []struct {
		name         string
		server       *v1alpha2.RemoteMCPServer
		agents       []*v1alpha2.Agent
		setupDB      func(*databasefake.InMemmoryFakeClient)
		expectError  bool
		errorMessage string
	}{
		{
			name: "successfully deletes server with no dependent agents",
			server: func() *v1alpha2.RemoteMCPServer {
				server := createTestRemoteMCPServer("test-server", "default")
				server.Finalizers = []string{RemoteMCPServerFinalizer}
				now := metav1.Now()
				server.DeletionTimestamp = &now
				return server
			}(),
			agents: []*v1alpha2.Agent{},
			setupDB: func(dbClient *databasefake.InMemmoryFakeClient) {
				// Pre-populate database with tool server to delete
				dbClient.StoreToolServer(&database.ToolServer{
					Name: "default/test-server",
				})
			},
			expectError: false,
		},
		{
			name: "prevents deletion when agents still reference server",
			server: func() *v1alpha2.RemoteMCPServer {
				server := createTestRemoteMCPServer("test-server", "default")
				server.Finalizers = []string{RemoteMCPServerFinalizer}
				now := metav1.Now()
				server.DeletionTimestamp = &now
				return server
			}(),
			agents: []*v1alpha2.Agent{
				func() *v1alpha2.Agent {
					agent := createTestAgent("agent1", "default", "test-model")
					agent.Spec.Tools = []*v1alpha2.Tool{
						{
							Type: v1alpha2.ToolProviderType_McpServer,
							McpServer: &v1alpha2.McpServerTool{
								TypedLocalReference: v1alpha2.TypedLocalReference{Name: "test-server"},
							},
						},
					}
					return agent
				}(),
			},
			setupDB:      func(dbClient *databasefake.InMemmoryFakeClient) {},
			expectError:  true,
			errorMessage: "still referenced by Agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{tt.server}
			for _, agent := range tt.agents {
				objects = append(objects, agent)
			}

			reconciler, _, dbClient, _ := setupTestReconciler(t, objects...)
			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.server.Name,
					Namespace: tt.server.Namespace,
				},
			}

			tt.setupDB(dbClient)

			err := reconciler.handleRemoteMCPServerDeletionWithFinalizer(ctx, tt.server, req)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFindAgentsUsingModelConfig(t *testing.T) {
	tests := []struct {
		name            string
		agents          []*v1alpha2.Agent
		modelConfigName types.NamespacedName
		expectedCount   int
	}{
		{
			name: "finds agents using specific modelconfig",
			agents: []*v1alpha2.Agent{
				createTestAgent("agent1", "default", "target-model"),
				createTestAgent("agent2", "default", "other-model"),
				createTestAgent("agent3", "default", "target-model"),
			},
			modelConfigName: types.NamespacedName{Name: "target-model", Namespace: "default"},
			expectedCount:   2,
		},
		{
			name: "finds no agents when none use the modelconfig",
			agents: []*v1alpha2.Agent{
				createTestAgent("agent1", "default", "other-model"),
			},
			modelConfigName: types.NamespacedName{Name: "target-model", Namespace: "default"},
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.agents))
			for i, agent := range tt.agents {
				objects[i] = agent
			}

			reconciler, _, _, _ := setupTestReconciler(t, objects...)
			ctx := context.Background()

			agents := reconciler.FindAgentsUsingModelConfig(ctx, tt.modelConfigName)

			assert.Len(t, agents, tt.expectedCount)
		})
	}
}

func TestFindAgentsUsingRemoteMCPServer(t *testing.T) {
	tests := []struct {
		name          string
		agents        []*v1alpha2.Agent
		serverName    types.NamespacedName
		expectedCount int
	}{
		{
			name: "finds agents using specific server",
			agents: []*v1alpha2.Agent{
				func() *v1alpha2.Agent {
					agent := createTestAgent("agent1", "default", "test-model")
					agent.Spec.Tools = []*v1alpha2.Tool{
						{
							Type: v1alpha2.ToolProviderType_McpServer,
							McpServer: &v1alpha2.McpServerTool{
								TypedLocalReference: v1alpha2.TypedLocalReference{Name: "target-server"},
							},
						},
					}
					return agent
				}(),
				func() *v1alpha2.Agent {
					agent := createTestAgent("agent2", "default", "test-model")
					agent.Spec.Tools = []*v1alpha2.Tool{
						{
							Type: v1alpha2.ToolProviderType_McpServer,
							McpServer: &v1alpha2.McpServerTool{
								TypedLocalReference: v1alpha2.TypedLocalReference{Name: "other-server"},
							},
						},
					}
					return agent
				}(),
			},
			serverName:    types.NamespacedName{Name: "target-server", Namespace: "default"},
			expectedCount: 1,
		},
		{
			name: "finds no agents when none use the server",
			agents: []*v1alpha2.Agent{
				func() *v1alpha2.Agent {
					agent := createTestAgent("agent1", "default", "test-model")
					agent.Spec.Tools = []*v1alpha2.Tool{
						{
							Type: v1alpha2.ToolProviderType_McpServer,
							McpServer: &v1alpha2.McpServerTool{
								TypedLocalReference: v1alpha2.TypedLocalReference{Name: "other-server"},
							},
						},
					}
					return agent
				}(),
			},
			serverName:    types.NamespacedName{Name: "target-server", Namespace: "default"},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.agents))
			for i, agent := range tt.agents {
				objects[i] = agent
			}

			reconciler, _, _, _ := setupTestReconciler(t, objects...)
			ctx := context.Background()

			agents := reconciler.FindAgentsUsingRemoteMCPServer(ctx, tt.serverName)

			assert.Len(t, agents, tt.expectedCount)
		})
	}
}
